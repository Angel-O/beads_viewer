package correlation

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	json "github.com/goccy/go-json"
	"gopkg.in/yaml.v3"
)

const workConfigVersion = 1

var fullCommitSHARegex = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

// WorkConfig keeps the private Beads store, source checkouts, and correlation
// ledger outside source repositories.
type WorkConfig struct {
	Version      int                             `yaml:"version"`
	Store        string                          `yaml:"store"`
	Ledger       string                          `yaml:"ledger"`
	Repositories map[string]WorkConfigRepository `yaml:"repositories"`
}

// WorkConfigStore resolves the authoritative Beads store from a work config.
func WorkConfigStore(configPath string) (string, error) {
	resolvedConfigPath, err := expandConfigPath(configPath)
	if err != nil {
		return "", fmt.Errorf("resolving work config: %w", err)
	}
	data, err := os.ReadFile(resolvedConfigPath)
	if err != nil {
		return "", fmt.Errorf("reading work config %q: %w", resolvedConfigPath, err)
	}
	var config WorkConfig
	if err := decodeWorkConfig(data, &config); err != nil {
		return "", fmt.Errorf("parsing work config %q: %w", resolvedConfigPath, err)
	}
	if config.Version != workConfigVersion {
		return "", fmt.Errorf("work config %q has unsupported version %d (supported: %d)", resolvedConfigPath, config.Version, workConfigVersion)
	}
	if err := validateWorkConfigRepositories(resolvedConfigPath, config.Repositories); err != nil {
		return "", err
	}
	store, err := resolvePrivatePath(config.Store, filepath.Dir(resolvedConfigPath))
	if err != nil {
		return "", fmt.Errorf("resolving work config store: %w", err)
	}
	if store == "" {
		return "", fmt.Errorf("work config %q requires a non-empty store path", resolvedConfigPath)
	}
	return store, nil
}

// AddExternalCorrelation resolves ref to an immutable source commit and writes
// one deduplicated JSONL record to the private ledger using atomic replacement.
func AddExternalCorrelation(configPath, beadID, repository, ref string) (ExternalHistoryCorrelation, bool, error) {
	resolvedConfigPath, err := expandConfigPath(configPath)
	if err != nil {
		return ExternalHistoryCorrelation{}, false, fmt.Errorf("resolving work config: %w", err)
	}
	data, err := os.ReadFile(resolvedConfigPath)
	if err != nil {
		return ExternalHistoryCorrelation{}, false, fmt.Errorf("reading work config %q: %w", resolvedConfigPath, err)
	}
	var config WorkConfig
	if err := decodeWorkConfig(data, &config); err != nil {
		return ExternalHistoryCorrelation{}, false, fmt.Errorf("parsing work config %q: %w", resolvedConfigPath, err)
	}
	if config.Version != workConfigVersion {
		return ExternalHistoryCorrelation{}, false, fmt.Errorf("work config %q has unsupported version %d (supported: %d)", resolvedConfigPath, config.Version, workConfigVersion)
	}
	if err := validateWorkConfigRepositories(resolvedConfigPath, config.Repositories); err != nil {
		return ExternalHistoryCorrelation{}, false, err
	}
	beadID = strings.TrimSpace(beadID)
	repository = strings.TrimSpace(repository)
	ref = strings.TrimSpace(ref)
	if beadID == "" || repository == "" || ref == "" {
		return ExternalHistoryCorrelation{}, false, fmt.Errorf("bead, repo, and commit must all be non-empty")
	}

	baseDir := filepath.Dir(resolvedConfigPath)
	ledger, err := resolvePrivatePath(config.Ledger, baseDir)
	if err != nil || ledger == "" {
		return ExternalHistoryCorrelation{}, false, fmt.Errorf("resolving correlation ledger: %w", err)
	}
	for _, contextKey := range sortedRepositoryKeys(config.Repositories) {
		configuredRepository := config.Repositories[contextKey]
		configuredPath, resolveErr := resolvePrivatePath(configuredRepository.Path, baseDir)
		if resolveErr != nil {
			return ExternalHistoryCorrelation{}, false, fmt.Errorf("resolving repository %q: %w", contextKey, resolveErr)
		}
		if pathWithin(configuredPath, ledger) {
			return ExternalHistoryCorrelation{}, false, fmt.Errorf("correlation ledger %q must not be inside source repository %q at %q", ledger, contextKey, configuredPath)
		}
	}
	contextKey, repoPath, err := resolveConfiguredRepository(config.Repositories, repository, baseDir)
	if err != nil {
		return ExternalHistoryCorrelation{}, false, err
	}
	if strings.ContainsAny(ref, "\x00\r\n") {
		return ExternalHistoryCorrelation{}, false, fmt.Errorf("invalid commit ref %q", ref)
	}
	store, err := resolvePrivatePath(config.Store, baseDir)
	if err != nil || store == "" {
		return ExternalHistoryCorrelation{}, false, fmt.Errorf("resolving work config store: %w", err)
	}
	if err := validateBeadContext(store, beadID, contextKey); err != nil {
		return ExternalHistoryCorrelation{}, false, err
	}
	cmd := gitCommand(nil, "-C", repoPath, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ExternalHistoryCorrelation{}, false, fmt.Errorf("resolving commit %q in context %q at %q: %w: %s", ref, contextKey, repoPath, err, strings.TrimSpace(string(out)))
	}
	fullSHA := strings.TrimSpace(string(out))
	if !fullCommitSHARegex.MatchString(fullSHA) {
		return ExternalHistoryCorrelation{}, false, fmt.Errorf("Git returned invalid commit SHA %q for ref %q", fullSHA, ref)
	}
	record := ExternalHistoryCorrelation{BeadID: beadID, Context: contextKey, Commit: strings.ToLower(fullSHA)}

	if err := os.MkdirAll(filepath.Dir(ledger), 0o700); err != nil {
		return ExternalHistoryCorrelation{}, false, fmt.Errorf("creating correlation ledger directory: %w", err)
	}
	lock, err := os.OpenFile(ledger+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return ExternalHistoryCorrelation{}, false, fmt.Errorf("opening correlation ledger lock: %w", err)
	}
	defer lock.Close()
	if err := lockFile(lock); err != nil {
		return ExternalHistoryCorrelation{}, false, fmt.Errorf("locking correlation ledger: %w", err)
	}
	defer func() { _ = unlockFile(lock) }()

	records, err := loadCorrelationLedgerIfExists(ledger)
	if err != nil {
		return ExternalHistoryCorrelation{}, false, err
	}
	for _, existing := range records {
		if existing.BeadID == record.BeadID && existing.Context == record.Context && strings.EqualFold(existing.Commit, record.Commit) {
			return record, false, nil
		}
	}
	records = append(records, record)
	if err := writeCorrelationLedgerAtomic(ledger, records); err != nil {
		return ExternalHistoryCorrelation{}, false, err
	}
	return record, true, nil
}

func resolveConfiguredRepository(repositories map[string]WorkConfigRepository, value, baseDir string) (string, string, error) {
	wantedPath := ""
	if strings.HasPrefix(value, "ctx:") {
		repository, ok := repositories[value]
		if !ok {
			return "", "", fmt.Errorf("repository context %q is not configured in the work config", value)
		}
		path, err := resolvePrivatePath(repository.Path, baseDir)
		if err != nil {
			return "", "", fmt.Errorf("resolving repository %q: %w", value, err)
		}
		return value, path, nil
	} else {
		var err error
		wantedPath, err = resolvePrivatePath(value, baseDir)
		if err != nil {
			return "", "", err
		}
	}
	for _, contextKey := range sortedRepositoryKeys(repositories) {
		repository := repositories[contextKey]
		path, err := resolvePrivatePath(repository.Path, baseDir)
		if err != nil {
			return "", "", fmt.Errorf("resolving repository %q: %w", contextKey, err)
		}
		if filepath.Clean(path) == filepath.Clean(wantedPath) {
			return contextKey, path, nil
		}
	}
	return "", "", fmt.Errorf("repository %q is not configured in the work config", value)
}

func loadCorrelationLedgerIfExists(path string) ([]ExternalHistoryCorrelation, error) {
	records, err := loadCorrelationLedger(path)
	if os.IsNotExist(unwrapPathError(err)) {
		if _, linkErr := os.Lstat(path); linkErr == nil {
			return nil, err
		} else if !os.IsNotExist(linkErr) {
			return nil, fmt.Errorf("checking correlation ledger %q: %w", path, linkErr)
		}
		return nil, nil
	}
	return records, err
}

func unwrapPathError(err error) error {
	for err != nil {
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return err
		}
		err = unwrapper.Unwrap()
	}
	return nil
}

func writeCorrelationLedgerAtomic(path string, records []ExternalHistoryCorrelation) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating correlation ledger directory %q: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".bv-correlations-*")
	if err != nil {
		return fmt.Errorf("creating temporary correlation ledger: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("securing temporary correlation ledger: %w", err)
	}
	encoder := json.NewEncoder(tmp)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			tmp.Close()
			return fmt.Errorf("encoding correlation ledger: %w", err)
		}
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing correlation ledger: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing correlation ledger: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replacing correlation ledger %q: %w", path, err)
	}
	directory, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("opening correlation ledger directory %q: %w", dir, err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("syncing correlation ledger directory %q: %w", dir, err)
	}
	return nil
}

// WorkConfigRepository configures one source checkout. Its map key is the ctx: label.
type WorkConfigRepository struct {
	Path string `yaml:"path"`
}

// ExternalHistoryCorrelation is one append-friendly private ledger record.
type ExternalHistoryCorrelation struct {
	BeadID  string `json:"bead_id"`
	Context string `json:"context"`
	Commit  string `json:"commit"`
}

type validatedExternalManifest struct {
	configPath   string
	store        string
	ledger       string
	repositories map[string]string
	correlations []ExternalHistoryCorrelation
}

func loadExternalHistoryManifest(path string, beads []BeadInfo) (*validatedExternalManifest, error) {
	configPath, err := expandConfigPath(path)
	if err != nil {
		return nil, fmt.Errorf("resolving work config %q: %w", path, err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading work config %q: %w", configPath, err)
	}

	var config WorkConfig
	if err := decodeWorkConfig(data, &config); err != nil {
		return nil, fmt.Errorf("parsing work config %q: %w", configPath, err)
	}
	if config.Version != workConfigVersion {
		return nil, fmt.Errorf("work config %q has unsupported version %d (supported: %d)", configPath, config.Version, workConfigVersion)
	}
	if err := validateWorkConfigRepositories(configPath, config.Repositories); err != nil {
		return nil, err
	}

	result := &validatedExternalManifest{
		configPath:   configPath,
		repositories: make(map[string]string, len(config.Repositories)),
	}
	baseDir := filepath.Dir(configPath)
	result.store, err = resolvePrivatePath(config.Store, baseDir)
	if err != nil {
		return nil, fmt.Errorf("work config %q store: %w", configPath, err)
	}
	result.ledger, err = resolvePrivatePath(config.Ledger, baseDir)
	if err != nil {
		return nil, fmt.Errorf("work config %q ledger: %w", configPath, err)
	}
	if result.store == "" || result.ledger == "" {
		return nil, fmt.Errorf("work config %q requires non-empty store and ledger paths", configPath)
	}
	for _, key := range sortedRepositoryKeys(config.Repositories) {
		repository := config.Repositories[key]
		path := strings.TrimSpace(repository.Path)
		path, err = resolvePrivatePath(path, baseDir)
		if err != nil {
			return nil, fmt.Errorf("work config %q repository %q: %w", configPath, key, err)
		}
		result.repositories[key] = filepath.Clean(path)
	}
	result.correlations, err = loadCorrelationLedgerIfExists(result.ledger)
	if err != nil {
		return nil, err
	}

	beadsByID := make(map[string]BeadInfo, len(beads))
	for _, bead := range beads {
		beadsByID[bead.ID] = bead
	}
	seen := make(map[string]struct{}, len(result.correlations))
	for i, correlation := range result.correlations {
		correlation.BeadID = strings.TrimSpace(correlation.BeadID)
		correlation.Context = strings.TrimSpace(correlation.Context)
		correlation.Commit = strings.TrimSpace(correlation.Commit)
		if correlation.BeadID == "" || correlation.Context == "" || correlation.Commit == "" {
			return nil, fmt.Errorf("correlation ledger %q record %d requires non-empty bead_id, context, and commit", result.ledger, i+1)
		}
		bead, exists := beadsByID[correlation.BeadID]
		if !exists {
			return nil, fmt.Errorf("correlation ledger %q record %d references unknown bead %q", result.ledger, i+1, correlation.BeadID)
		}
		if _, exists := result.repositories[correlation.Context]; !exists {
			return nil, fmt.Errorf("correlation ledger %q record %d references undefined context %q", result.ledger, i+1, correlation.Context)
		}
		if !containsString(bead.Labels, correlation.Context) {
			return nil, fmt.Errorf("correlation ledger %q record %d maps bead %q to %q, but the bead does not carry that context label", result.ledger, i+1, correlation.BeadID, correlation.Context)
		}
		if !fullCommitSHARegex.MatchString(correlation.Commit) {
			return nil, fmt.Errorf("correlation ledger %q record %d commit %q must be a full 40- or 64-character Git object ID", result.ledger, i+1, correlation.Commit)
		}
		identity := correlation.BeadID + "\x00" + correlation.Context + "\x00" + strings.ToLower(correlation.Commit)
		if _, exists := seen[identity]; exists {
			return nil, fmt.Errorf("correlation ledger %q repeats correlation for bead %q, context %q, commit %q", result.ledger, correlation.BeadID, correlation.Context, correlation.Commit)
		}
		seen[identity] = struct{}{}
		result.correlations[i] = correlation
	}

	return result, nil
}

func sortedRepositoryKeys(repositories map[string]WorkConfigRepository) []string {
	keys := make([]string, 0, len(repositories))
	for key := range repositories {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func validateWorkConfigRepositories(configPath string, repositories map[string]WorkConfigRepository) error {
	for _, key := range sortedRepositoryKeys(repositories) {
		if key != strings.TrimSpace(key) || !strings.HasPrefix(key, "ctx:") || len(key) == len("ctx:") {
			return fmt.Errorf("work config %q has invalid repository context key %q: expected a ctx:<repo>-<hash> label", configPath, key)
		}
		if strings.TrimSpace(repositories[key].Path) == "" {
			return fmt.Errorf("work config %q repository %q has an empty path", configPath, key)
		}
	}
	return nil
}

func decodeWorkConfig(data []byte, config *WorkConfig) error {
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	return decoder.Decode(config)
}

func pathWithin(parent, child string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func loadCorrelationLedger(path string) ([]ExternalHistoryCorrelation, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading correlation ledger %q: %w", path, err)
	}
	defer file.Close()

	var records []ExternalHistoryCorrelation
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var record ExternalHistoryCorrelation
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("parsing correlation ledger %q line %d: %w", path, line, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading correlation ledger %q: %w", path, err)
	}
	return records, nil
}

func expandConfigPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	} else if strings.HasPrefix(path, "~") {
		return "", fmt.Errorf("unsupported home path %q; use ~/...", path)
	}
	return filepath.Abs(path)
}

func resolvePrivatePath(path, baseDir string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		return expandConfigPath(path)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	return filepath.Abs(path)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
