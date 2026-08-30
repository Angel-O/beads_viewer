package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
	"gopkg.in/yaml.v3"
)

type migrationSourceIssue struct {
	ID     string   `json:"id"`
	Labels []string `json:"labels"`
}

type migrationCorrelation struct {
	OldID  string `json:"old_id"`
	Commit string `json:"commit"`
}

type migrationPlan struct {
	config       hub.ResolvedConfig
	source       string
	destination  string
	context      string
	prefix       string
	issues       []migrationSourceIssue
	sourceDigest string
	correlations []migrationCorrelation
	backupPath   string
	labels       []string
}

type migrationBackendResult struct {
	SchemaVersion      int               `json:"schema_version"`
	Source             string            `json:"source"`
	Destination        string            `json:"destination"`
	Digest             string            `json:"digest"`
	Applied            bool              `json:"applied"`
	IssuesImported     int               `json:"issues_imported"`
	HistoryImported    int               `json:"history_imported"`
	EventsImported     int               `json:"events_imported"`
	ProvenanceImported int               `json:"provenance_imported"`
	IssueMap           map[string]string `json:"issue_map"`
}

type migrationOutput struct {
	Phase                 string                      `json:"phase"`
	Source                string                      `json:"source"`
	Destination           string                      `json:"destination"`
	Context               string                      `json:"context"`
	Prefix                string                      `json:"prefix"`
	SourceIssueCount      int                         `json:"source_issue_count"`
	SourceDigest          string                      `json:"source_digest"`
	ExactCorrelationCount int                         `json:"exact_correlation_count"`
	Labels                []string                    `json:"labels"`
	BackupPath            string                      `json:"backup_path"`
	Backend               *migrationBackendResult     `json:"backend,omitempty"`
	Correlations          migrationCorrelationSummary `json:"correlations"`
	Verification          *migrationVerification      `json:"verification,omitempty"`
}

type migrationCorrelationSummary struct {
	Planned  int `json:"planned"`
	Added    int `json:"added"`
	Existing int `json:"existing"`
	Total    int `json:"total"`
}

type migrationVerification struct {
	IssuesVerified  int  `json:"issues_verified"`
	SourceUnchanged bool `json:"source_unchanged"`
}

func (a *app) migrate(request request) int {
	plan, err := a.migrationPreflight()
	if err != nil {
		return a.fail(err)
	}
	if request.migrateDryRun {
		return a.writeJSON(migrationOutput{
			Phase:                 "dry-run",
			Source:                plan.source,
			Destination:           plan.destination,
			Context:               plan.context,
			Prefix:                plan.prefix,
			SourceIssueCount:      len(plan.issues),
			SourceDigest:          plan.sourceDigest,
			ExactCorrelationCount: len(plan.correlations),
			Labels:                plan.labels,
			BackupPath:            plan.backupPath,
			Correlations:          migrationCorrelationSummary{Planned: len(plan.correlations)},
		})
	}

	backupPath, err := createMigrationBackup(plan)
	if err != nil {
		return a.fail(fmt.Errorf("creating migration backup: %w", err))
	}
	data, diagnostic, err := a.runBDCaptureAtStore(a.dir, plan.destination, "--json", "store-copy", plan.source, plan.destination,
		"--prefix", plan.prefix, "--namespace", plan.context, "--label", "imported", "--label", plan.context)
	if err != nil {
		if detail := strings.TrimSpace(string(diagnostic)); detail != "" {
			return a.fail(fmt.Errorf("copying local store into Hub: %w: %s", err, detail))
		}
		return a.fail(fmt.Errorf("copying local store into Hub: %w", err))
	}
	backend, err := decodeMigrationBackend(data)
	if err != nil {
		return a.fail(fmt.Errorf("decoding store-copy result: %w", err))
	}
	if err := validateMigrationBackend(plan, backend); err != nil {
		return a.fail(err)
	}

	correlations := migrationCorrelationSummary{Planned: len(plan.correlations)}
	for _, planned := range plan.correlations {
		newID := backend.IssueMap[planned.OldID]
		_, added, correlationErr := correlation.AddExternalCorrelation(plan.config.Path, newID, plan.context, planned.Commit)
		if correlationErr != nil {
			return a.fail(fmt.Errorf("adding correlation for %q/%s: incomplete apply: %w", planned.OldID, planned.Commit, correlationErr))
		}
		if added {
			correlations.Added++
		} else {
			correlations.Existing++
		}
	}
	correlations.Total = correlations.Added + correlations.Existing

	verified, err := a.verifyMigration(plan, backend)
	if err != nil {
		return a.fail(err)
	}
	unchanged, err := migrationTreeDigest(plan.source)
	if err != nil {
		return a.fail(fmt.Errorf("verifying source immutability: %w", err))
	}
	if unchanged != plan.sourceDigest {
		return a.fail(errors.New("source store changed during migration"))
	}

	paths := a.paths
	paths.Store = plan.destination
	paths.Config = plan.config.Path
	paths.Ledger = plan.config.Ledger
	if err := hub.SignalChange(paths); err != nil {
		fmt.Fprintf(a.stderr, "wbd: warning: migration succeeded but Viewer notification failed: %v\n", err)
	}
	return a.writeJSON(migrationOutput{
		Phase:                 "apply",
		Source:                plan.source,
		Destination:           plan.destination,
		Context:               plan.context,
		Prefix:                plan.prefix,
		SourceIssueCount:      len(plan.issues),
		SourceDigest:          plan.sourceDigest,
		ExactCorrelationCount: len(plan.correlations),
		Labels:                plan.labels,
		BackupPath:            backupPath,
		Backend:               backend,
		Correlations:          correlations,
		Verification:          &migrationVerification{IssuesVerified: verified, SourceUnchanged: true},
	})
}

func (a *app) migrationPreflight() (migrationPlan, error) {
	if err := migrationRegularFile(a.paths.Config, "Hub config"); err != nil {
		return migrationPlan{}, err
	}
	config, err := hub.Resolve(a.paths.Config)
	if err != nil {
		return migrationPlan{}, fmt.Errorf("resolving Hub config: %w", err)
	}
	if err := migrationOptionalRegularFile(config.Ledger, "correlation ledger"); err != nil {
		return migrationPlan{}, err
	}
	context, err := hub.Context(a.dir)
	if err != nil {
		return migrationPlan{}, err
	}
	root, err := hub.DurableRepositoryRoot(a.dir)
	if err != nil {
		return migrationPlan{}, err
	}
	repository, ok := config.Repositories[context]
	if !ok {
		return migrationPlan{}, fmt.Errorf("current repository context %q is not registered in the Hub", context)
	}
	configuredRoot, err := migrationCanonicalDirectory(repository.Path, "registered repository")
	if err != nil {
		return migrationPlan{}, err
	}
	if configuredRoot != root {
		return migrationPlan{}, fmt.Errorf("registered repository %q resolves to %q, want durable root %q", context, configuredRoot, root)
	}

	source, err := migrationStoreDirectory(filepath.Join(root, ".beads"), "source store")
	if err != nil {
		return migrationPlan{}, err
	}
	destination, err := migrationStoreDirectory(config.Store, "Hub store")
	if err != nil {
		return migrationPlan{}, err
	}
	if migrationPathsOverlap(source, destination) {
		return migrationPlan{}, errors.New("source and Hub stores must be distinct and non-overlapping")
	}
	if err := migrationRejectSymlinks(source, "source store"); err != nil {
		return migrationPlan{}, err
	}
	if err := migrationRejectSymlinks(destination, "Hub store"); err != nil {
		return migrationPlan{}, err
	}
	if err := validateMigrationStoreBackend(source, "source store"); err != nil {
		return migrationPlan{}, err
	}
	if err := validateMigrationStoreBackend(destination, "Hub store"); err != nil {
		return migrationPlan{}, err
	}

	prefix, err := a.migrationIssuePrefix(destination)
	if err != nil {
		return migrationPlan{}, err
	}
	issues, err := a.migrationSourceIssues(source)
	if err != nil {
		return migrationPlan{}, err
	}
	for _, issue := range issues {
		for _, label := range issue.Labels {
			if strings.HasPrefix(strings.TrimSpace(label), "ctx:") {
				return migrationPlan{}, fmt.Errorf("source issue %q carries reserved context label %q", issue.ID, label)
			}
		}
	}
	digest, err := migrationTreeDigest(source)
	if err != nil {
		return migrationPlan{}, fmt.Errorf("digesting source store: %w", err)
	}
	correlations, err := discoverMigrationCorrelations(root, issues)
	if err != nil {
		return migrationPlan{}, err
	}
	return migrationPlan{
		config:       config,
		source:       source,
		destination:  destination,
		context:      context,
		prefix:       prefix,
		issues:       issues,
		sourceDigest: digest,
		correlations: correlations,
		backupPath:   migrationBackupCandidate(filepath.Dir(destination)),
		labels:       []string{"imported", context},
	}, nil
}

func (a *app) migrationIssuePrefix(store string) (string, error) {
	data, _, err := a.runBDCaptureAtStore(a.dir, store, "--readonly", "--json", "config", "get", "issue_prefix")
	if err != nil {
		return "", fmt.Errorf("reading Hub issue prefix: %w", err)
	}
	var result struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("decoding Hub issue prefix: %w", err)
	}
	if err := validatePrefix(strings.TrimSpace(result.Value)); err != nil {
		return "", fmt.Errorf("invalid Hub issue prefix: %w", err)
	}
	return strings.TrimSpace(result.Value), nil
}

func (a *app) migrationSourceIssues(store string) ([]migrationSourceIssue, error) {
	data, _, err := a.runBDCaptureAtStore(a.dir, store, "--readonly", "--json", "list", "--all", "--include-all-types", "--limit", "0")
	if err != nil {
		return nil, fmt.Errorf("reading source issues: %w", err)
	}
	var issues []migrationSourceIssue
	if err := json.Unmarshal(data, &issues); err != nil {
		return nil, fmt.Errorf("decoding source issues: %w", err)
	}
	seen := make(map[string]struct{}, len(issues))
	for _, issue := range issues {
		if issue.ID == "" || strings.TrimSpace(issue.ID) != issue.ID || strings.ContainsAny(issue.ID, "\r\n\t") {
			return nil, errors.New("source issue list contains an invalid issue ID")
		}
		if _, ok := seen[issue.ID]; ok {
			return nil, fmt.Errorf("source issue list repeats issue ID %q", issue.ID)
		}
		seen[issue.ID] = struct{}{}
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].ID < issues[j].ID })
	return issues, nil
}

func discoverMigrationCorrelations(repository string, issues []migrationSourceIssue) ([]migrationCorrelation, error) {
	patterns := make([]*regexp.Regexp, 0, len(issues))
	canonicalIDs := make(map[string]string, len(issues))
	for _, issue := range issues {
		pattern := regexp.MustCompile(`(?i)(?:^|[^[:alnum:]_-])(` + regexp.QuoteMeta(issue.ID) + `)(?:$|[^[:alnum:]_-])`)
		patterns = append(patterns, pattern)
		key := strings.ToLower(issue.ID)
		if existing, ok := canonicalIDs[key]; ok && existing != issue.ID {
			return nil, fmt.Errorf("source issue IDs %q and %q differ only by case", existing, issue.ID)
		}
		canonicalIDs[key] = issue.ID
	}

	data, err := migrationGitHistory(repository)
	if err != nil {
		return nil, fmt.Errorf("discovering migration correlations: %w", err)
	}
	matcher := correlation.NewExplicitMatcherWithPatterns(repository, patterns)
	var result []migrationCorrelation
	for _, commit := range data {
		for _, found := range matcher.ExtractIDsFromMessage(commit.Message) {
			if canonical, ok := canonicalIDs[strings.ToLower(found.ID)]; ok {
				result = append(result, migrationCorrelation{OldID: canonical, Commit: commit.SHA})
			}
		}
	}
	seen := make(map[string]struct{}, len(result))
	filtered := result[:0]
	for _, item := range result {
		key := item.OldID + "\x00" + item.Commit
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		filtered = append(filtered, item)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].OldID == filtered[j].OldID {
			return filtered[i].Commit < filtered[j].Commit
		}
		return filtered[i].OldID < filtered[j].OldID
	})
	return filtered, nil
}

type migrationGitCommit struct {
	SHA     string
	Message string
}

func migrationGitHistory(repository string) ([]migrationGitCommit, error) {
	cmd := exec.Command("git", "-C", repository, "log", "--format=%H%x00%B%x00")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, fmt.Errorf("git log failed: %w: %s", err, message)
		}
		return nil, fmt.Errorf("git log failed: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	fields := bytes.Split(data, []byte{0})
	if len(fields)%2 != 1 {
		return nil, errors.New("git log returned malformed NUL-delimited records")
	}
	commits := make([]migrationGitCommit, 0, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		sha := string(bytes.TrimLeft(fields[i], "\r\n"))
		if !isFullCommitSHA(sha) {
			return nil, fmt.Errorf("git log returned invalid commit SHA %q", sha)
		}
		commits = append(commits, migrationGitCommit{SHA: strings.ToLower(sha), Message: string(fields[i+1])})
	}
	if len(bytes.TrimSpace(fields[len(fields)-1])) != 0 {
		return nil, errors.New("git log returned an unterminated record")
	}
	return commits, nil
}

func decodeMigrationBackend(data []byte) (*migrationBackendResult, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var result migrationBackendResult
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for _, field := range []string{"schema_version", "source", "destination", "digest", "applied", "issues_imported", "history_imported", "events_imported", "provenance_imported", "issue_map"} {
		value, ok := fields[field]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, fmt.Errorf("store-copy result is missing %s", field)
		}
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("store-copy returned multiple JSON values")
		}
		return nil, err
	}
	if result.SchemaVersion != 1 {
		return nil, fmt.Errorf("store-copy result has unsupported schema_version %d", result.SchemaVersion)
	}
	return &result, nil
}

func validateMigrationBackend(plan migrationPlan, result *migrationBackendResult) error {
	if filepath.Clean(result.Source) != plan.source || filepath.Clean(result.Destination) != plan.destination {
		return errors.New("store-copy returned different source or destination")
	}
	if result.Digest == "" {
		return errors.New("store-copy returned an empty digest")
	}
	if result.IssuesImported < 0 || result.HistoryImported < 0 || result.EventsImported < 0 || result.ProvenanceImported < 0 {
		return errors.New("store-copy returned a negative import count")
	}
	if len(result.IssueMap) != len(plan.issues) {
		return fmt.Errorf("store-copy issue map has %d entries, want %d", len(result.IssueMap), len(plan.issues))
	}
	known := make(map[string]struct{}, len(plan.issues))
	destinations := make(map[string]struct{}, len(plan.issues))
	destinationPattern := regexp.MustCompile("^" + regexp.QuoteMeta(plan.prefix) + `-[0-9a-f]{64}$`)
	for _, issue := range plan.issues {
		known[issue.ID] = struct{}{}
		mapped, ok := result.IssueMap[issue.ID]
		if !ok || mapped == "" {
			return fmt.Errorf("store-copy issue map is missing source issue %q", issue.ID)
		}
		if !destinationPattern.MatchString(mapped) {
			return fmt.Errorf("store-copy issue map has invalid destination issue ID %q", mapped)
		}
		if _, duplicate := destinations[mapped]; duplicate {
			return fmt.Errorf("store-copy issue map repeats destination issue %q", mapped)
		}
		destinations[mapped] = struct{}{}
	}
	for oldID := range result.IssueMap {
		if _, ok := known[oldID]; !ok {
			return fmt.Errorf("store-copy issue map contains unknown source issue %q", oldID)
		}
	}
	if result.Applied && result.IssuesImported != len(plan.issues) {
		return fmt.Errorf("applied store-copy imported %d issues, want %d", result.IssuesImported, len(plan.issues))
	}
	if !result.Applied && (result.IssuesImported != 0 || result.HistoryImported != 0 || result.EventsImported != 0 || result.ProvenanceImported != 0) {
		return errors.New("no-op store-copy reported imported data")
	}
	return nil
}

func (a *app) verifyMigration(plan migrationPlan, result *migrationBackendResult) (int, error) {
	data, _, err := a.runBDCaptureAtStore(a.dir, plan.destination, "--readonly", "--json", "list", "--all", "--include-all-types", "--limit", "0")
	if err != nil {
		return 0, fmt.Errorf("verifying imported issues: %w", err)
	}
	var issues []bdIssue
	if err := json.Unmarshal(data, &issues); err != nil {
		return 0, fmt.Errorf("decoding imported issues: %w", err)
	}
	byID := make(map[string]bdIssue, len(issues))
	for _, issue := range issues {
		if _, exists := byID[issue.ID]; exists {
			return 0, fmt.Errorf("verification returned duplicate issue %q", issue.ID)
		}
		byID[issue.ID] = issue
	}
	for _, source := range plan.issues {
		id := result.IssueMap[source.ID]
		issue, ok := byID[id]
		if !ok {
			return 0, fmt.Errorf("verifying imported issue %q returned no record", id)
		}
		if !containsString(issue.Labels, "imported") || !containsString(issue.Labels, plan.context) {
			return 0, fmt.Errorf("imported issue %q is missing required labels", id)
		}
		for _, label := range source.Labels {
			if !strings.HasPrefix(strings.TrimSpace(label), "ctx:") && !containsString(issue.Labels, label) {
				return 0, fmt.Errorf("imported issue %q is missing source label %q", id, label)
			}
		}
	}
	return len(plan.issues), nil
}

func migrationStoreDirectory(path, name string) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%s is missing: %s", name, path)
	}
	if err != nil {
		return "", fmt.Errorf("inspecting %s: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%s must not be a symlink: %s", name, path)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory: %s", name, path)
	}
	return migrationCanonicalDirectory(path, name)
}

func migrationCanonicalDirectory(path, name string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("canonicalizing %s: %w", name, err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", name, err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory: %s", name, path)
	}
	return resolved, nil
}

func migrationRegularFile(path, name string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspecting %s: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular non-symlink file: %s", name, path)
	}
	return nil
}

func migrationOptionalRegularFile(path, name string) error {
	if path == "" {
		return nil
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspecting %s: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular non-symlink file: %s", name, path)
	}
	return nil
}

func migrationRejectSymlinks(root, name string) error {
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s contains a symlink: %s", name, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("inspecting %s: %w", name, err)
	}
	return nil
}

func validateMigrationStoreBackend(store, name string) error {
	for _, fileName := range []string{"metadata.json", "config.json"} {
		path := filepath.Join(store, fileName)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspecting %s metadata: %w", name, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("%s metadata must not be a symlink or special file: %s", name, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s metadata: %w", name, err)
		}
		var metadata struct {
			Backend          string `json:"backend"`
			DoltMode         string `json:"dolt_mode"`
			DoltServerHost   string `json:"dolt_server_host"`
			DoltServerPort   int    `json:"dolt_server_port"`
			DoltServerSocket string `json:"dolt_server_socket"`
			DoltTeamServer   bool   `json:"dolt_team_server"`
		}
		if err := json.Unmarshal(data, &metadata); err != nil {
			return fmt.Errorf("decoding %s metadata: %w", name, err)
		}
		if metadata.Backend != "" && metadata.Backend != "dolt" {
			return fmt.Errorf("%s uses unsupported storage backend %q", name, metadata.Backend)
		}
		mode := strings.ToLower(strings.TrimSpace(metadata.DoltMode))
		if mode != "" && mode != "embedded" || metadata.DoltTeamServer || metadata.DoltServerSocket != "" || metadata.DoltServerPort != 0 || strings.TrimSpace(metadata.DoltServerHost) != "" {
			return fmt.Errorf("%s uses unsupported server-mode storage", name)
		}
	}
	doltConfig := make(map[string]any)
	for _, fileName := range []string{"config.yaml", "config.local.yaml"} {
		configPath := filepath.Join(store, fileName)
		if err := migrationOptionalRegularFile(configPath, name+" config"); err != nil {
			return err
		}
		data, err := os.ReadFile(configPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("reading %s config: %w", name, err)
		}
		var root map[string]any
		if err := yaml.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("decoding %s config: %w", name, err)
		}
		if rawDolt, ok := root["dolt"]; ok {
			dolt, ok := rawDolt.(map[string]any)
			if !ok {
				return fmt.Errorf("%s config dolt section is not a mapping", name)
			}
			for key, value := range dolt {
				doltConfig[key] = value
			}
		}
	}
	mode := ""
	if value, ok := doltConfig["mode"]; ok && value != nil {
		mode = strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	}
	shared := false
	if value, ok := doltConfig["shared-server"]; ok && value != nil {
		shared = strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), "true")
	}
	if mode != "" && mode != "embedded" || shared {
		return fmt.Errorf("%s uses unsupported server-mode storage", name)
	}
	for _, key := range []string{"host", "port"} {
		if value, ok := doltConfig[key]; ok && value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
			return fmt.Errorf("%s config sets dolt.%s, which is unsupported server-mode storage", name, key)
		}
	}
	return nil
}

func migrationPathsOverlap(first, second string) bool {
	return migrationPathContains(first, second) || migrationPathContains(second, first)
}

func migrationPathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func migrationBackupCandidate(parent string) string {
	return filepath.Join(parent, "wbd-migrate-backup-"+time.Now().UTC().Format("20060102T150405.000000000Z"))
}

func createMigrationBackup(plan migrationPlan) (string, error) {
	for suffix := 0; ; suffix++ {
		path := plan.backupPath
		if suffix > 0 {
			path += fmt.Sprintf("-%d", suffix+1)
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", err
		}
		if err := copyMigrationTree(plan.source, filepath.Join(path, "source", ".beads")); err != nil {
			return path, err
		}
		if err := copyMigrationTree(plan.destination, filepath.Join(path, "hub", ".beads")); err != nil {
			return path, err
		}
		if err := copyMigrationFile(plan.config.Path, filepath.Join(path, "hub.yaml")); err != nil {
			return path, err
		}
		if info, err := os.Lstat(plan.config.Ledger); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return path, fmt.Errorf("correlation ledger must be a regular non-symlink file: %s", plan.config.Ledger)
			}
			if err := copyMigrationFile(plan.config.Ledger, filepath.Join(path, "correlations.jsonl")); err != nil {
				return path, err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return path, fmt.Errorf("inspecting correlation ledger: %w", err)
		}
		if err := writeMigrationChecksums(path); err != nil {
			return path, err
		}
		return path, nil
	}
}

func copyMigrationTree(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	return copyMigrationDirectory(source, destination)
}

func copyMigrationDirectory(source, destination string) error {
	if err := os.Mkdir(destination, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		sourcePath := filepath.Join(source, entry.Name())
		destinationPath := filepath.Join(destination, entry.Name())
		info, err := os.Lstat(sourcePath)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to follow symlink in backup: %s", sourcePath)
		}
		if info.IsDir() {
			if err := copyMigrationDirectory(sourcePath, destinationPath); err != nil {
				return err
			}
			continue
		}
		if err := copyMigrationFile(sourcePath, destinationPath); err != nil {
			return err
		}
	}
	return nil
}

func copyMigrationFile(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("backup source must be a regular non-symlink file: %s", source)
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}

type migrationFileDigest struct {
	path string
	sum  string
}

func migrationFileDigests(root string) ([]migrationFileDigest, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to follow symlink: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("source contains a non-regular file: %s", path)
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	result := make([]migrationFileDigest, 0, len(files))
	for _, path := range files {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		result = append(result, migrationFileDigest{path: filepath.ToSlash(relative), sum: hex.EncodeToString(hash.Sum(nil))})
	}
	return result, nil
}

func migrationTreeDigest(root string) (string, error) {
	files, err := migrationFileDigests(root)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	for _, file := range files {
		_, _ = io.WriteString(hash, file.path)
		_, _ = hash.Write([]byte{0})
		_, _ = io.WriteString(hash, file.sum)
		_, _ = hash.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeMigrationChecksums(root string) error {
	files, err := migrationFileDigests(root)
	if err != nil {
		return err
	}
	path := filepath.Join(root, "checksums.sha256")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	for _, entry := range files {
		if _, err := fmt.Fprintf(file, "%s  %s\n", entry.sum, entry.path); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
