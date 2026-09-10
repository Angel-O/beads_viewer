package hub

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
)

var fullExternalCommitSHA = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

type correlationLedgerEntry struct {
	correlation correlation.ExternalHistoryCorrelation
	raw         []byte
}

type correlationLedgerWriter func(string, []correlationLedgerEntry) (bool, error)

// StorePath resolves the authoritative Beads store from a Hub config.
func StorePath(configPath string) (string, error) {
	resolvedPath, err := ResolvePath(configPath, "")
	if err != nil {
		return "", fmt.Errorf("resolving hub config: %w", err)
	}
	config, err := Load(resolvedPath)
	if err != nil {
		return "", err
	}
	store, err := ResolvePath(config.Store, filepath.Dir(resolvedPath))
	if err != nil {
		return "", fmt.Errorf("resolving hub config store: %w", err)
	}
	if store == "" {
		return "", fmt.Errorf("hub config %q requires a non-empty store path", resolvedPath)
	}
	return store, nil
}

// NewExternalHistorySource adapts validated Hub config and ledger state to the
// neutral correlation source boundary.
func NewExternalHistorySource(configPath string) correlation.ExternalHistorySource {
	return func(beads []correlation.BeadInfo) (correlation.ExternalHistorySnapshot, error) {
		return loadExternalHistory(configPath, beads)
	}
}

// AddExternalCorrelation resolves a ref and atomically adds one ledger record.
func AddExternalCorrelation(configPath, beadID, repository, ref string) (correlation.ExternalHistoryCorrelation, bool, error) {
	return addExternalCorrelation(configPath, beadID, repository, ref, writeCorrelationLedgerAtomic)
}

func addExternalCorrelation(configPath, beadID, repository, ref string, writeLedger correlationLedgerWriter) (correlation.ExternalHistoryCorrelation, bool, error) {
	config, baseDir, err := loadCorrelationConfig(configPath)
	if err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, err
	}
	beadID, repository, ref = strings.TrimSpace(beadID), strings.TrimSpace(repository), strings.TrimSpace(ref)
	if beadID == "" || repository == "" || ref == "" {
		return correlation.ExternalHistoryCorrelation{}, false, fmt.Errorf("bead, repo, and commit must all be non-empty")
	}
	ledger, err := ResolvePath(config.Ledger, baseDir)
	if err != nil || ledger == "" {
		return correlation.ExternalHistoryCorrelation{}, false, fmt.Errorf("resolving correlation ledger: %w", err)
	}
	if err := validateLedgerLocation(ledger, config.Repositories, baseDir); err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, err
	}
	contextKey, repoPath, err := resolveConfiguredRepository(config.Repositories, repository, baseDir)
	if err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, err
	}
	if strings.ContainsAny(ref, "\x00\r\n") {
		return correlation.ExternalHistoryCorrelation{}, false, fmt.Errorf("invalid commit ref %q", ref)
	}
	store, err := ResolvePath(config.Store, baseDir)
	if err != nil || store == "" {
		return correlation.ExternalHistoryCorrelation{}, false, fmt.Errorf("resolving hub config store: %w", err)
	}
	if err := validateBeadContext(store, beadID, contextKey); err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, err
	}
	out, err := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}").CombinedOutput()
	if err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, fmt.Errorf("resolving commit %q in context %q at %q: %w: %s", ref, contextKey, repoPath, err, strings.TrimSpace(string(out)))
	}
	fullSHA := strings.TrimSpace(string(out))
	if !fullExternalCommitSHA.MatchString(fullSHA) {
		return correlation.ExternalHistoryCorrelation{}, false, fmt.Errorf("Git returned invalid commit SHA %q for ref %q", fullSHA, ref)
	}
	record := correlation.ExternalHistoryCorrelation{BeadID: beadID, Context: contextKey, Commit: strings.ToLower(fullSHA)}
	return appendExternalCorrelation(ledger, record, writeLedger)
}

// RemoveExternalCorrelation removes one exact logical association atomically.
func RemoveExternalCorrelation(configPath, beadID, repository, commit string) (correlation.ExternalHistoryCorrelation, bool, error) {
	return removeExternalCorrelation(configPath, beadID, repository, commit, writeCorrelationLedgerAtomic)
}

func removeExternalCorrelation(configPath, beadID, repository, commit string, writeLedger correlationLedgerWriter) (correlation.ExternalHistoryCorrelation, bool, error) {
	config, baseDir, err := loadCorrelationConfig(configPath)
	if err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, err
	}
	beadID, repository, commit = strings.TrimSpace(beadID), strings.TrimSpace(repository), strings.TrimSpace(commit)
	if beadID == "" || repository == "" || commit == "" {
		return correlation.ExternalHistoryCorrelation{}, false, fmt.Errorf("bead, repo, and commit must all be non-empty")
	}
	if !fullExternalCommitSHA.MatchString(commit) {
		return correlation.ExternalHistoryCorrelation{}, false, fmt.Errorf("commit %q must be a full 40- or 64-character Git object ID", commit)
	}
	ledger, err := ResolvePath(config.Ledger, baseDir)
	if err != nil || ledger == "" {
		return correlation.ExternalHistoryCorrelation{}, false, fmt.Errorf("resolving correlation ledger: %w", err)
	}
	if err := validateLedgerLocation(ledger, config.Repositories, baseDir); err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, err
	}
	contextKey, _, err := resolveConfiguredRepository(config.Repositories, repository, baseDir)
	if err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, err
	}
	store, err := ResolvePath(config.Store, baseDir)
	if err != nil || store == "" {
		return correlation.ExternalHistoryCorrelation{}, false, fmt.Errorf("resolving hub config store: %w", err)
	}
	if err := validateBeadContext(store, beadID, contextKey); err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, err
	}
	record := correlation.ExternalHistoryCorrelation{BeadID: beadID, Context: contextKey, Commit: strings.ToLower(commit)}
	if err := os.MkdirAll(filepath.Dir(ledger), 0o700); err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, fmt.Errorf("creating correlation ledger directory: %w", err)
	}
	lock, err := os.OpenFile(ledger+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, fmt.Errorf("opening correlation ledger lock: %w", err)
	}
	defer lock.Close()
	if err := lockLedgerFile(lock); err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, fmt.Errorf("locking correlation ledger: %w", err)
	}
	defer func() { _ = unlockLedgerFile(lock) }()
	entries, err := loadCorrelationLedgerIfExists(ledger)
	if err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, err
	}
	if err := validateCorrelationLedgerForRemoval(ledger, entries, config.Repositories, store, record); err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, err
	}
	kept := make([]correlationLedgerEntry, 0, len(entries))
	removed := false
	for _, entry := range entries {
		if sameCorrelation(entry.correlation, record) {
			removed = true
			continue
		}
		kept = append(kept, entry)
	}
	if !removed {
		return record, false, nil
	}
	committed, err := writeLedger(ledger, kept)
	if err != nil {
		if committed {
			return record, true, err
		}
		return correlation.ExternalHistoryCorrelation{}, false, err
	}
	return record, true, nil
}

func appendExternalCorrelation(ledger string, record correlation.ExternalHistoryCorrelation, writeLedger correlationLedgerWriter) (correlation.ExternalHistoryCorrelation, bool, error) {
	if err := os.MkdirAll(filepath.Dir(ledger), 0o700); err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, fmt.Errorf("creating correlation ledger directory: %w", err)
	}
	lock, err := os.OpenFile(ledger+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, fmt.Errorf("opening correlation ledger lock: %w", err)
	}
	defer lock.Close()
	if err := lockLedgerFile(lock); err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, fmt.Errorf("locking correlation ledger: %w", err)
	}
	defer func() { _ = unlockLedgerFile(lock) }()
	entries, err := loadCorrelationLedgerIfExists(ledger)
	if err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, err
	}
	for _, entry := range entries {
		if sameCorrelation(entry.correlation, record) {
			return record, false, nil
		}
	}
	data, err := json.Marshal(record)
	if err != nil {
		return correlation.ExternalHistoryCorrelation{}, false, fmt.Errorf("encoding correlation ledger record: %w", err)
	}
	entries = append(entries, correlationLedgerEntry{correlation: record, raw: data})
	committed, err := writeLedger(ledger, entries)
	if err != nil {
		if committed {
			return record, true, err
		}
		return correlation.ExternalHistoryCorrelation{}, false, err
	}
	return record, true, nil
}

func loadExternalHistory(configPath string, beads []correlation.BeadInfo) (correlation.ExternalHistorySnapshot, error) {
	config, err := Resolve(configPath)
	if err != nil {
		return correlation.ExternalHistorySnapshot{}, err
	}
	if config.Store == "" || config.Ledger == "" {
		return correlation.ExternalHistorySnapshot{}, fmt.Errorf("hub config %q requires non-empty store and ledger paths", config.Path)
	}
	entries, err := loadCorrelationLedgerIfExists(config.Ledger)
	if err != nil {
		return correlation.ExternalHistorySnapshot{}, err
	}
	beadsByID := make(map[string]correlation.BeadInfo, len(beads))
	for _, bead := range beads {
		beadsByID[bead.ID] = bead
	}
	seen := make(map[string]struct{}, len(entries))
	records := make([]correlation.ExternalHistoryCorrelation, 0, len(entries))
	repositories := make(map[string]string, len(config.Repositories))
	for contextKey, repository := range config.Repositories {
		repositories[contextKey] = filepath.Clean(repository.Path)
	}
	for i, entry := range entries {
		record := entry.correlation
		record.BeadID, record.Context, record.Commit = strings.TrimSpace(record.BeadID), strings.TrimSpace(record.Context), strings.TrimSpace(record.Commit)
		if record.BeadID == "" || record.Context == "" || record.Commit == "" {
			return correlation.ExternalHistorySnapshot{}, fmt.Errorf("correlation ledger %q record %d requires non-empty bead_id, context, and commit", config.Ledger, i+1)
		}
		bead, exists := beadsByID[record.BeadID]
		if !exists {
			continue
		}
		if _, exists := repositories[record.Context]; !exists {
			return correlation.ExternalHistorySnapshot{}, fmt.Errorf("correlation ledger %q record %d references undefined context %q", config.Ledger, i+1, record.Context)
		}
		if !containsString(bead.Labels, record.Context) {
			return correlation.ExternalHistorySnapshot{}, fmt.Errorf("correlation ledger %q record %d maps bead %q to %q, but the bead does not carry that context label", config.Ledger, i+1, record.BeadID, record.Context)
		}
		if !fullExternalCommitSHA.MatchString(record.Commit) {
			return correlation.ExternalHistorySnapshot{}, fmt.Errorf("correlation ledger %q record %d commit %q must be a full 40- or 64-character Git object ID", config.Ledger, i+1, record.Commit)
		}
		identity := record.BeadID + "\x00" + record.Context + "\x00" + strings.ToLower(record.Commit)
		if _, duplicate := seen[identity]; duplicate {
			return correlation.ExternalHistorySnapshot{}, fmt.Errorf("correlation ledger %q repeats correlation for bead %q, context %q, commit %q", config.Ledger, record.BeadID, record.Context, record.Commit)
		}
		seen[identity] = struct{}{}
		records = append(records, record)
	}
	return correlation.ExternalHistorySnapshot{Store: config.Store, Ledger: config.Ledger, Repositories: repositories, Correlations: records}, nil
}

func loadCorrelationConfig(path string) (Config, string, error) {
	resolved, err := ResolvePath(path, "")
	if err != nil {
		return Config{}, "", fmt.Errorf("resolving hub config: %w", err)
	}
	config, err := Load(resolved)
	if err != nil {
		return Config{}, "", err
	}
	return config, filepath.Dir(resolved), nil
}

func validateLedgerLocation(ledger string, repositories map[string]Repository, baseDir string) error {
	for _, contextKey := range sortedRepositoryKeys(repositories) {
		repository := repositories[contextKey]
		path, err := ResolvePath(repository.Path, baseDir)
		if err != nil {
			return fmt.Errorf("resolving repository %q: %w", contextKey, err)
		}
		if pathWithin(path, ledger) {
			return fmt.Errorf("correlation ledger %q must not be inside source repository %q at %q", ledger, contextKey, path)
		}
	}
	return nil
}

func resolveConfiguredRepository(repositories map[string]Repository, value, baseDir string) (string, string, error) {
	if strings.HasPrefix(value, "ctx:") {
		repository, ok := repositories[value]
		if !ok {
			return "", "", fmt.Errorf("repository context %q is not configured in the hub config", value)
		}
		path, err := ResolvePath(repository.Path, baseDir)
		if err != nil {
			return "", "", fmt.Errorf("resolving repository %q: %w", value, err)
		}
		return value, path, nil
	}
	wantedPath, err := ResolvePath(value, baseDir)
	if err != nil {
		return "", "", err
	}
	for _, contextKey := range sortedRepositoryKeys(repositories) {
		path, err := ResolvePath(repositories[contextKey].Path, baseDir)
		if err != nil {
			return "", "", fmt.Errorf("resolving repository %q: %w", contextKey, err)
		}
		if filepath.Clean(path) == filepath.Clean(wantedPath) {
			return contextKey, path, nil
		}
	}
	return "", "", fmt.Errorf("repository %q is not configured in the hub config", value)
}

func validateBeadContext(store, beadID, contextKey string) error {
	cmd := exec.Command("bd", "--db", store, "--readonly", "show", beadID, "--json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("validating bead %q in configured store %q: %w: %s", beadID, store, err, strings.TrimSpace(string(out)))
	}
	var issues []struct {
		ID        string   `json:"id"`
		IssueType string   `json:"issue_type"`
		Labels    []string `json:"labels"`
	}
	if err := json.Unmarshal(out, &issues); err != nil {
		return fmt.Errorf("parsing bead %q from configured store %q: %w", beadID, store, err)
	}
	if len(issues) == 0 || issues[0].ID != beadID {
		return fmt.Errorf("bead %q was not found in configured store %q", beadID, store)
	}
	if strings.TrimSpace(issues[0].IssueType) == "" {
		return fmt.Errorf("bead %q does not provide a non-empty issue_type", beadID)
	}
	if strings.EqualFold(issues[0].IssueType, "todo") {
		return fmt.Errorf("bead %q is a todo and cannot be correlated with a Git commit", beadID)
	}
	for _, label := range issues[0].Labels {
		if label == contextKey {
			return nil
		}
	}
	return fmt.Errorf("bead %q in configured store %q does not carry context label %q", beadID, store, contextKey)
}

func loadCorrelationLedgerIfExists(path string) ([]correlationLedgerEntry, error) {
	entries, err := loadCorrelationLedger(path)
	if os.IsNotExist(unwrapPathError(err)) {
		if _, linkErr := os.Lstat(path); linkErr == nil {
			return nil, err
		} else if !os.IsNotExist(linkErr) {
			return nil, fmt.Errorf("checking correlation ledger %q: %w", path, linkErr)
		}
		return nil, nil
	}
	return entries, err
}

func loadCorrelationLedger(path string) ([]correlationLedgerEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading correlation ledger %q: %w", path, err)
	}
	defer file.Close()
	var entries []correlationLedgerEntry
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var record correlation.ExternalHistoryCorrelation
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("parsing correlation ledger %q line %d: %w", path, line, err)
		}
		entries = append(entries, correlationLedgerEntry{correlation: record, raw: append([]byte(nil), scanner.Bytes()...)})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading correlation ledger %q: %w", path, err)
	}
	return entries, nil
}

func writeCorrelationLedgerAtomic(path string, entries []correlationLedgerEntry) (bool, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, fmt.Errorf("creating correlation ledger directory %q: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".bv-correlations-*")
	if err != nil {
		return false, fmt.Errorf("creating temporary correlation ledger: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return false, fmt.Errorf("securing temporary correlation ledger: %w", err)
	}
	writer := bufio.NewWriter(tmp)
	for _, entry := range entries {
		if _, err := writer.Write(entry.raw); err != nil {
			tmp.Close()
			return false, fmt.Errorf("writing correlation ledger: %w", err)
		}
		if err := writer.WriteByte('\n'); err != nil {
			tmp.Close()
			return false, fmt.Errorf("writing correlation ledger: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		tmp.Close()
		return false, fmt.Errorf("writing correlation ledger: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return false, fmt.Errorf("syncing correlation ledger: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("closing correlation ledger: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return false, fmt.Errorf("replacing correlation ledger %q: %w", path, err)
	}
	directory, err := os.Open(dir)
	if err != nil {
		return true, fmt.Errorf("opening correlation ledger directory %q after replacement: %w", dir, err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return true, fmt.Errorf("syncing correlation ledger directory %q after replacement: %w", dir, err)
	}
	return true, nil
}

func validateCorrelationLedgerForRemoval(ledger string, entries []correlationLedgerEntry, repositories map[string]Repository, store string, target correlation.ExternalHistoryCorrelation) error {
	targetIdentity := target.BeadID + "\x00" + target.Context + "\x00" + strings.ToLower(target.Commit)
	seen := make(map[string]struct{}, len(entries))
	validated := make(map[string]struct{})
	for i, entry := range entries {
		record := entry.correlation
		record.BeadID, record.Context, record.Commit = strings.TrimSpace(record.BeadID), strings.TrimSpace(record.Context), strings.TrimSpace(record.Commit)
		if record.BeadID == "" || record.Context == "" || record.Commit == "" {
			return fmt.Errorf("correlation ledger %q record %d requires non-empty bead_id, context, and commit", ledger, i+1)
		}
		if _, ok := repositories[record.Context]; !ok {
			return fmt.Errorf("correlation ledger %q record %d references undefined context %q", ledger, i+1, record.Context)
		}
		if !fullExternalCommitSHA.MatchString(record.Commit) {
			return fmt.Errorf("correlation ledger %q record %d commit %q must be a full 40- or 64-character Git object ID", ledger, i+1, record.Commit)
		}
		beadContext := record.BeadID + "\x00" + record.Context
		if _, ok := validated[beadContext]; !ok {
			if err := validateBeadContext(store, record.BeadID, record.Context); err != nil {
				return fmt.Errorf("correlation ledger %q record %d: %w", ledger, i+1, err)
			}
			validated[beadContext] = struct{}{}
		}
		identity := beadContext + "\x00" + strings.ToLower(record.Commit)
		if _, duplicate := seen[identity]; duplicate && identity != targetIdentity {
			return fmt.Errorf("correlation ledger %q repeats correlation for bead %q, context %q, commit %q", ledger, record.BeadID, record.Context, record.Commit)
		}
		seen[identity] = struct{}{}
	}
	return nil
}

func sameCorrelation(a, b correlation.ExternalHistoryCorrelation) bool {
	return strings.TrimSpace(a.BeadID) == b.BeadID && strings.TrimSpace(a.Context) == b.Context && strings.EqualFold(strings.TrimSpace(a.Commit), b.Commit)
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

func pathWithin(parent, child string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	return err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))))
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
