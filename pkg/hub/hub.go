// Package hub implements the repository-to-Hub integration shared by wbd and wbv.
package hub

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"gopkg.in/yaml.v3"
)

// ConfigVersion is the supported Hub configuration schema version.
const ConfigVersion = 1

const changeSignalName = "viewer-generation"

const semanticCacheDirName = "semantic"

// PolicyCode is a stable machine-readable Hub admission or lifecycle error.
type PolicyCode string

const (
	PolicyInvalidKind         PolicyCode = "invalid_kind"
	PolicyInvalidCardinality  PolicyCode = "invalid_cardinality"
	PolicyUnregisteredContext PolicyCode = "unregistered_context"
	PolicyReservedLabel       PolicyCode = "reserved_context_label"
	PolicyInvalidTodoResult   PolicyCode = "invalid_todo_result"
	PolicyTodoCorrelation     PolicyCode = "todo_correlation"
	PolicyInvalidEpicChild    PolicyCode = "invalid_epic_child"
	PolicyInvalidSupersession PolicyCode = "invalid_supersession"
)

// PolicyError describes a rejected Hub state without depending on a writer.
type PolicyError struct {
	Code    PolicyCode `json:"code"`
	Field   string     `json:"field,omitempty"`
	Value   string     `json:"value,omitempty"`
	Message string     `json:"message"`
}

func (e *PolicyError) Error() string { return e.Message }

// KindClass is the Hub policy category for an issue type.
type KindClass uint8

const (
	KindUnsupported KindClass = iota
	KindProjectWork
	KindTodo
	KindEpic
	KindDecision
)

// IssueState contains the authoritative issue facts used by Hub policy.
type IssueState struct {
	ID     string
	Kind   string
	Status string
	Labels []string
}

// AdmittedIssue is a complete validated issue type and label set.
type AdmittedIssue struct {
	Kind     string
	Contexts []string
	Labels   []string
}

// ClassifyKind maps only the issue types accepted by the Hub writer.
func ClassifyKind(kind string) (KindClass, error) {
	switch kind {
	case "task", "bug", "feature", "chore":
		return KindProjectWork, nil
	case "todo":
		return KindTodo, nil
	case "epic":
		return KindEpic, nil
	case "decision":
		return KindDecision, nil
	default:
		return KindUnsupported, &PolicyError{
			Code: PolicyInvalidKind, Field: "type", Value: kind,
			Message: fmt.Sprintf("unsupported Hub issue type %q", kind),
		}
	}
}

// Contexts extracts a sorted, de-duplicated set of reserved context labels.
func Contexts(labels []string) []string {
	seen := make(map[string]struct{})
	for _, label := range labels {
		if strings.HasPrefix(label, "ctx:") {
			seen[label] = struct{}{}
		}
	}
	contexts := make([]string, 0, len(seen))
	for context := range seen {
		contexts = append(contexts, context)
	}
	sort.Strings(contexts)
	return contexts
}

// AdmitIssue validates the complete proposed membership before persistence.
// labels must contain ordinary caller-owned labels only; contexts are supplied
// separately by target resolution.
func AdmitIssue(kind string, contexts, labels []string, registered map[string]Repository) (AdmittedIssue, error) {
	class, err := ClassifyKind(kind)
	if err != nil {
		return AdmittedIssue{}, err
	}
	for _, label := range labels {
		if strings.HasPrefix(strings.TrimLeft(label, " "), "ctx:") {
			return AdmittedIssue{}, &PolicyError{
				Code: PolicyReservedLabel, Field: "labels", Value: label,
				Message: "ctx: labels are wrapper-owned and immutable",
			}
		}
	}

	contexts = uniqueSorted(contexts)
	if err := ValidateRegisteredContexts(contexts, registered); err != nil {
		return AdmittedIssue{}, err
	}
	if err := validateCardinality(kind, class, len(contexts)); err != nil {
		return AdmittedIssue{}, err
	}

	completeLabels := append([]string(nil), labels...)
	completeLabels = append(completeLabels, contexts...)
	return AdmittedIssue{Kind: kind, Contexts: contexts, Labels: completeLabels}, nil
}

// ValidateStoredIssue checks kind, context registration, and cardinality for
// an authoritative record without interpreting ordinary labels.
func ValidateStoredIssue(issue IssueState, registered map[string]Repository) error {
	return validateMembership(issue.Kind, Contexts(issue.Labels), registered)
}

// ValidateRegisteredContexts rejects the first unregistered context in sorted
// order, giving callers deterministic structured errors.
func ValidateRegisteredContexts(contexts []string, registered map[string]Repository) error {
	for _, context := range uniqueSorted(contexts) {
		if _, ok := registered[context]; !ok {
			return &PolicyError{
				Code: PolicyUnregisteredContext, Field: "context", Value: context,
				Message: fmt.Sprintf("context %q is not registered", context),
			}
		}
	}
	return nil
}

// ValidateCardinality checks the immutable membership count for a supported
// issue type.
func ValidateCardinality(kind string, count int) error {
	class, err := ClassifyKind(kind)
	if err != nil {
		return err
	}
	return validateCardinality(kind, class, count)
}

// ValidateTodoResult requires todo -> ordinary project-work continuity.
func ValidateTodoResult(todo, result IssueState) error {
	todoClass, todoErr := ClassifyKind(todo.Kind)
	resultClass, resultErr := ClassifyKind(result.Kind)
	if todoErr != nil || resultErr != nil || todoClass != KindTodo || resultClass != KindProjectWork || todo.ID == result.ID {
		return &PolicyError{
			Code: PolicyInvalidTodoResult, Field: "from_todo", Value: todo.ID,
			Message: "todo results require a todo source and ordinary project-work result",
		}
	}
	return nil
}

// ValidateCorrelationOwner prevents capture-only todos from owning commits.
func ValidateCorrelationOwner(issue IssueState) error {
	class, err := ClassifyKind(issue.Kind)
	if err != nil {
		return err
	}
	if class == KindTodo {
		return &PolicyError{
			Code: PolicyTodoCorrelation, Field: "bead", Value: issue.ID,
			Message: "todo cannot own a direct commit correlation",
		}
	}
	return nil
}

// ValidateEpicChild preserves ordinary parent-child behavior for non-epic
// parents and constrains only epic coordination.
func ValidateEpicChild(parent, child IssueState) error {
	parentClass, err := ClassifyKind(parent.Kind)
	if err != nil || parentClass != KindEpic {
		return nil
	}
	childClass, childErr := ClassifyKind(child.Kind)
	childContexts := Contexts(child.Labels)
	parentContexts := Contexts(parent.Labels)
	if childErr != nil || childClass != KindProjectWork || len(childContexts) != 1 || !contains(parentContexts, childContexts[0]) {
		return &PolicyError{
			Code: PolicyInvalidEpicChild, Field: "parent", Value: parent.ID,
			Message: "an epic may parent only ordinary project work in one of its contexts",
		}
	}
	return nil
}

// ValidateSupersession requires a replacement and original of the same
// supported issue type. Membership is validated independently at admission.
func ValidateSupersession(replacement, original IssueState) error {
	_, replacementErr := ClassifyKind(replacement.Kind)
	_, originalErr := ClassifyKind(original.Kind)
	if replacementErr != nil || originalErr != nil || replacement.Kind != original.Kind || replacement.ID == original.ID {
		return &PolicyError{
			Code: PolicyInvalidSupersession, Field: "supersedes", Value: original.ID,
			Message: "replacement and original must be distinct and have the same supported type",
		}
	}
	return nil
}

func validateMembership(kind string, contexts []string, registered map[string]Repository) error {
	class, err := ClassifyKind(kind)
	if err != nil {
		return err
	}
	if err := ValidateRegisteredContexts(contexts, registered); err != nil {
		return err
	}
	return validateCardinality(kind, class, len(contexts))
}

func validateCardinality(kind string, class KindClass, count int) error {
	valid := class == KindTodo || class == KindEpic && count >= 1 ||
		(class == KindProjectWork || class == KindDecision) && count == 1
	if !valid {
		return &PolicyError{
			Code: PolicyInvalidCardinality, Field: "context", Value: kind,
			Message: cardinalityMessage(kind, count),
		}
	}
	return nil
}

func cardinalityMessage(kind string, count int) string {
	switch kind {
	case "todo":
		return "todo permits zero or more contexts"
	case "epic":
		return fmt.Sprintf("epic requires one or more contexts (got %d)", count)
	default:
		return fmt.Sprintf("%s requires exactly one context (got %d)", kind, count)
	}
}

func contains(values []string, wanted string) bool {
	index := sort.SearchStrings(values, wanted)
	return index < len(values) && values[index] == wanted
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// Paths identifies the fixed files and directories used by a Hub.
type Paths struct {
	Store  string
	Config string
	Ledger string
}

// Repository is a repository registered for a context.
type Repository struct {
	Path string `json:"path" yaml:"path"`
}

// Config is the version-1 Hub configuration.
type Config struct {
	Version      int                   `json:"version" yaml:"version"`
	Store        string                `json:"store" yaml:"store"`
	Ledger       string                `json:"ledger" yaml:"ledger"`
	Repositories map[string]Repository `json:"repositories" yaml:"repositories"`
}

// ResolvedConfig is a validated Hub config whose paths are absolute.
type ResolvedConfig struct {
	Path         string
	Store        string
	Ledger       string
	Repositories map[string]Repository
}

// Registration describes the context and durable path registered for a repository.
type Registration struct {
	Context string
	Root    string
	Changed bool
}

// LoadRepositoryCatalog loads only Hub configuration and builds repository
// metadata from the complete issue set. It never opens the correlation ledger.
func LoadRepositoryCatalog(path string, issues []model.Issue) (model.RepositoryCatalog, error) {
	config, err := Resolve(path)
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int, len(config.Repositories))
	for _, issue := range issues {
		seen := make(map[string]bool)
		for _, label := range issue.Labels {
			if _, registered := config.Repositories[label]; registered && !seen[label] {
				counts[label]++
				seen[label] = true
			}
		}
	}

	names := shortestUniqueRepositoryNames(config.Repositories)
	catalog := make(model.RepositoryCatalog, 0, len(config.Repositories))
	for _, context := range sortedRepositoryKeys(config.Repositories) {
		path := config.Repositories[context].Path
		catalog = append(catalog, model.RepositoryCatalogEntry{
			ID:        context,
			Name:      names[context],
			Path:      path,
			Detail:    path,
			BeadCount: counts[context],
			Kind:      model.RepositoryIdentityHubContext,
		})
	}
	model.SortRepositoryCatalog(catalog)
	return catalog, nil
}

func shortestUniqueRepositoryNames(repositories map[string]Repository) map[string]string {
	components := make(map[string][]string, len(repositories))
	groups := make(map[string][]string)
	for context, repository := range repositories {
		cleaned := filepath.Clean(repository.Path)
		volume := filepath.VolumeName(cleaned)
		trimmed := strings.TrimPrefix(cleaned, volume)
		trimmed = strings.Trim(trimmed, string(filepath.Separator))
		parts := strings.FieldsFunc(trimmed, func(r rune) bool { return r == '/' || r == '\\' })
		if volume != "" {
			volumeParts := strings.FieldsFunc(volume, func(r rune) bool { return r == '/' || r == '\\' })
			parts = append(volumeParts, parts...)
		}
		if len(parts) == 0 {
			parts = []string{cleaned}
		}
		components[context] = parts
		base := parts[len(parts)-1]
		groups[base] = append(groups[base], context)
	}

	names := make(map[string]string, len(repositories))
	for base, contexts := range groups {
		sort.Strings(contexts)
		if len(contexts) == 1 {
			names[contexts[0]] = base
			continue
		}
		for _, context := range contexts {
			parts := components[context]
			name := strings.Join(parts, "/")
			for depth := 2; depth <= len(parts); depth++ {
				candidate := strings.Join(parts[len(parts)-depth:], "/")
				unique := true
				for _, other := range contexts {
					if other == context {
						continue
					}
					otherParts := components[other]
					otherDepth := depth
					if otherDepth > len(otherParts) {
						otherDepth = len(otherParts)
					}
					if candidate == strings.Join(otherParts[len(otherParts)-otherDepth:], "/") {
						unique = false
						break
					}
				}
				if unique {
					name = candidate
					break
				}
			}
			if duplicateRepositoryPath(context, contexts, repositories) {
				name += " (" + context + ")"
			}
			names[context] = name
		}
	}
	return names
}

func duplicateRepositoryPath(context string, contexts []string, repositories map[string]Repository) bool {
	path := filepath.Clean(repositories[context].Path)
	for _, other := range contexts {
		if other != context && filepath.Clean(repositories[other].Path) == path {
			return true
		}
	}
	return false
}

// DefaultPaths returns the Hub paths rooted at the current user's home directory.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolving user home directory: %w", err)
	}
	parent := filepath.Join(home, ".local", "share", "beads", "hub")
	return Paths{
		Store:  filepath.Join(parent, ".beads"),
		Config: filepath.Join(home, ".config", "bv", "hub.yaml"),
		Ledger: filepath.Join(parent, "correlations.jsonl"),
	}, nil
}

// ChangeSignalPath returns the application-owned file used to notify Viewer of
// successful Hub mutations. It lives beside the store, outside repositories.
func ChangeSignalPath(paths Paths) string {
	return filepath.Join(filepath.Dir(paths.Store), changeSignalName)
}

// SemanticCacheDir returns the application-owned directory for the Hub's
// generated semantic search indexes. It lives beside the private Hub store.
func SemanticCacheDir(paths Paths) string {
	return filepath.Join(filepath.Dir(paths.Store), semanticCacheDirName)
}

// SignalChange atomically advances the Viewer change signal after a successful
// Hub mutation. The random generation makes every replacement observable even
// when mutations occur within the filesystem timestamp resolution.
func SignalChange(paths Paths) error {
	path := ChangeSignalPath(paths)
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+changeSignalName+".")
	if err != nil {
		return fmt.Errorf("creating Hub change signal: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		if keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("setting Hub change signal permissions: %w", err)
	}
	var generation [16]byte
	if _, err := rand.Read(generation[:]); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("generating Hub change signal: %w", err)
	}
	if _, err := fmt.Fprintf(temporary, "%x\n", generation); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("writing Hub change signal: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("closing Hub change signal: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("installing Hub change signal: %w", err)
	}
	keepTemporary = false
	return nil
}

// Load reads and validates a strict version-1 Hub config. Paths in the returned
// config retain their configured representation.
func Load(path string) (Config, error) {
	config, _, err := load(path)
	return config, err
}

// Resolve reads a Hub config and resolves its store, ledger, and repository
// paths relative to the config directory.
func Resolve(path string) (ResolvedConfig, error) {
	config, configPath, err := load(path)
	if err != nil {
		return ResolvedConfig{}, err
	}
	baseDir := filepath.Dir(configPath)
	resolved := ResolvedConfig{
		Path:         configPath,
		Repositories: make(map[string]Repository, len(config.Repositories)),
	}
	resolved.Store, err = ResolvePath(config.Store, baseDir)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("hub config %q store: %w", configPath, err)
	}
	resolved.Ledger, err = ResolvePath(config.Ledger, baseDir)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("hub config %q ledger: %w", configPath, err)
	}
	for _, context := range sortedRepositoryKeys(config.Repositories) {
		repository := config.Repositories[context]
		repository.Path, err = ResolvePath(repository.Path, baseDir)
		if err != nil {
			return ResolvedConfig{}, fmt.Errorf("hub config %q repository %q: %w", configPath, context, err)
		}
		resolved.Repositories[context] = repository
	}
	return resolved, nil
}

// ResolvePath expands ~/ and makes a path absolute. Relative paths are based
// at baseDir; an empty baseDir resolves relative to the current directory.
func ResolvePath(path, baseDir string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
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
	} else if !filepath.IsAbs(path) && baseDir != "" {
		path = filepath.Join(baseDir, path)
	}
	return filepath.Abs(path)
}

func load(path string) (Config, string, error) {
	configPath, err := ResolvePath(path, "")
	if err != nil {
		return Config{}, "", fmt.Errorf("resolving hub config %q: %w", path, err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, "", fmt.Errorf("reading hub config %q: %w", configPath, err)
	}
	config, err := parseConfig(data, configPath)
	if err != nil {
		return Config{}, "", fmt.Errorf("parsing hub config %q: %w", configPath, err)
	}
	return config, configPath, nil
}

// EnsureConfig creates or normalizes a strict version-1 Hub config.
func EnsureConfig(paths Paths) (Config, error) {
	if err := validatePaths(paths); err != nil {
		return Config{}, err
	}
	if err := rejectUnsafeConfig(paths.Config); err != nil {
		return Config{}, err
	}
	if err := os.MkdirAll(filepath.Dir(paths.Config), 0o700); err != nil {
		return Config{}, fmt.Errorf("creating hub config directory: %w", err)
	}

	config := Config{
		Version:      ConfigVersion,
		Store:        paths.Store,
		Ledger:       paths.Ledger,
		Repositories: make(map[string]Repository),
	}
	data, err := os.ReadFile(paths.Config)
	if err == nil {
		config, err = parseConfig(data, paths.Config)
		if err != nil {
			return Config{}, fmt.Errorf("loading hub config %s: %w", paths.Config, err)
		}
		config.Store = paths.Store
		config.Ledger = paths.Ledger
	} else if !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("reading hub config %s: %w", paths.Config, err)
	}

	if err := writeConfig(paths.Config, config); err != nil {
		return Config{}, err
	}
	return config, nil
}

// Context returns the origin-derived context identity for the Git worktree at dir.
func Context(dir string) (string, error) {
	inside, err := gitOutput(dir, "rev-parse", "--is-inside-work-tree")
	if err != nil || inside != "true" {
		return "", errors.New("current directory is not inside a Git work tree")
	}
	origin, err := gitOutput(dir, "config", "--get", "remote.origin.url")
	if err != nil || origin == "" {
		return "", errors.New("current repository has no origin remote")
	}

	host, path, err := originIdentity(origin)
	if err != nil {
		return "", err
	}
	identity := strings.ToLower(host + "/" + path)
	repositoryName := path[strings.LastIndex(path, "/")+1:]
	slug := pathSlug(repositoryName)
	if slug == "" {
		return "", errors.New("origin remote has no path-safe repository name")
	}
	digest := sha256.Sum256([]byte(identity))
	return "ctx:" + slug + "-" + hex.EncodeToString(digest[:])[:10], nil
}

// DurableRepositoryRoot returns the primary checkout for a linked worktree when
// it is available and belongs to the same Git common directory. Otherwise it
// safely falls back to the current checkout.
func DurableRepositoryRoot(dir string) (string, error) {
	currentRoot, err := gitOutput(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", errors.New("cannot resolve the current Git repository root")
	}
	if currentRoot == "" {
		return "", errors.New("current Git repository root is empty")
	}
	if hasUnsupportedControl(currentRoot) {
		return "", errors.New("repository path contains an unsupported control character")
	}
	currentRoot, err = canonicalDirectory(currentRoot)
	if err != nil {
		return "", fmt.Errorf("cannot canonicalize the current Git repository root: %w", err)
	}
	currentCommon, err := gitCommonDirectory(currentRoot)
	if err != nil {
		return "", err
	}

	worktrees, err := gitOutput(currentRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return "", errors.New("cannot list Git worktrees")
	}
	firstLine := worktrees
	if newline := strings.IndexByte(firstLine, '\n'); newline >= 0 {
		firstLine = firstLine[:newline]
	}
	if !strings.HasPrefix(firstLine, "worktree ") {
		return "", errors.New("malformed Git worktree list output")
	}
	primaryRoot := strings.TrimPrefix(firstLine, "worktree ")
	if primaryRoot == "" {
		return "", errors.New("malformed Git worktree list output")
	}
	if hasUnsupportedControl(primaryRoot) || !filepath.IsAbs(primaryRoot) {
		return "", errors.New("malformed Git worktree list path")
	}

	if info, statErr := os.Stat(primaryRoot); statErr == nil && info.IsDir() {
		canonicalPrimary, canonicalErr := canonicalDirectory(primaryRoot)
		if canonicalErr != nil {
			return "", fmt.Errorf("cannot canonicalize the primary Git worktree path: %w", canonicalErr)
		}
		inside, _ := gitOutput(canonicalPrimary, "rev-parse", "--is-inside-work-tree")
		bare, _ := gitOutput(canonicalPrimary, "rev-parse", "--is-bare-repository")
		if inside == "true" && bare == "false" {
			primaryCommon, commonErr := gitCommonDirectory(canonicalPrimary)
			if commonErr != nil {
				return "", commonErr
			}
			if primaryCommon == currentCommon {
				return canonicalPrimary, nil
			}
		}
	}
	return currentRoot, nil
}

// RepositoryIdentity returns the canonical worktree root and Git common
// directory for the repository containing dir.
func RepositoryIdentity(dir string) (string, string, error) {
	root, err := gitOutput(dir, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return "", "", errors.New("cannot resolve the current Git repository root")
	}
	root, err = canonicalDirectory(root)
	if err != nil {
		return "", "", fmt.Errorf("cannot canonicalize the current Git repository root: %w", err)
	}
	common, err := gitCommonDirectory(root)
	if err != nil {
		return "", "", err
	}
	return root, common, nil
}

// Register ensures the Hub config and registers the repository at dir.
func Register(paths Paths, dir string) (Registration, error) {
	context, err := Context(dir)
	if err != nil {
		return Registration{}, err
	}
	root, err := DurableRepositoryRoot(dir)
	if err != nil {
		return Registration{}, err
	}
	config, err := EnsureConfig(paths)
	if err != nil {
		return Registration{}, err
	}
	previous, exists := config.Repositories[context]
	changed := !exists || previous.Path != root
	config.Repositories[context] = Repository{Path: root}
	if err := writeConfig(paths.Config, config); err != nil {
		return Registration{}, err
	}
	return Registration{Context: context, Root: root, Changed: changed}, nil
}

// Configure registers the current repository when eligible and otherwise only
// ensures the Hub config. A nil registration indicates the config-only case.
func Configure(paths Paths, dir string) (*Registration, error) {
	inside, err := gitOutput(dir, "rev-parse", "--is-inside-work-tree")
	if err == nil && inside == "true" {
		if _, contextErr := Context(dir); contextErr == nil {
			registration, registerErr := Register(paths, dir)
			if registerErr != nil {
				return nil, registerErr
			}
			return &registration, nil
		}
	}
	_, err = EnsureConfig(paths)
	return nil, err
}

func validatePaths(paths Paths) error {
	if paths.Store == "" || paths.Config == "" || paths.Ledger == "" {
		return errors.New("hub paths must not be empty")
	}
	return nil
}

func rejectUnsafeConfig(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspecting hub config %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("hub config must not be a symlink: %s", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("hub config must be a regular file: %s", path)
	}
	return nil
}

func parseConfig(data []byte, configPath string) (Config, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, errors.New("multiple YAML documents")
		}
		return Config{}, err
	}
	if config.Version != ConfigVersion {
		return Config{}, fmt.Errorf("hub config %q has unsupported version %d (supported: %d)", configPath, config.Version, ConfigVersion)
	}
	if config.Repositories == nil {
		config.Repositories = make(map[string]Repository)
	}
	if err := validateRepositories(configPath, config.Repositories); err != nil {
		return Config{}, err
	}
	return config, nil
}

func sortedRepositoryKeys(repositories map[string]Repository) []string {
	keys := make([]string, 0, len(repositories))
	for key := range repositories {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func validateRepositories(configPath string, repositories map[string]Repository) error {
	for _, key := range sortedRepositoryKeys(repositories) {
		if key != strings.TrimSpace(key) || !strings.HasPrefix(key, "ctx:") || len(key) == len("ctx:") {
			return fmt.Errorf("hub config %q has invalid repository context key %q: expected a ctx:<repo>-<hash> label", configPath, key)
		}
		if strings.TrimSpace(repositories[key].Path) == "" {
			return fmt.Errorf("hub config %q repository %q has an empty path", configPath, key)
		}
	}
	return nil
}

func writeConfig(path string, config Config) error {
	data, err := marshalConfig(config)
	if err != nil {
		return fmt.Errorf("encoding hub config: %w", err)
	}
	if err := rejectUnsafeConfig(path); err != nil {
		return err
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, filepath.Base(path)+".tmp.")
	if err != nil {
		return fmt.Errorf("creating temporary hub config: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		if keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("setting temporary hub config permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("writing temporary hub config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("closing temporary hub config: %w", err)
	}

	existing, readErr := os.ReadFile(path)
	if readErr == nil && bytes.Equal(existing, data) {
		if err := rejectUnsafeConfig(path); err != nil {
			return err
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("setting hub config permissions: %w", err)
		}
		return nil
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("reading existing hub config: %w", readErr)
	}
	if err := rejectUnsafeConfig(path); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replacing hub config: %w", err)
	}
	keepTemporary = false
	return nil
}

func marshalConfig(config Config) ([]byte, error) {
	// Struct field order matches jq -S's lexicographic object-key ordering.
	wire := struct {
		Ledger       string                `json:"ledger"`
		Repositories map[string]Repository `json:"repositories"`
		Store        string                `json:"store"`
		Version      int                   `json:"version"`
	}{config.Ledger, config.Repositories, config.Store, config.Version}
	if wire.Repositories == nil {
		wire.Repositories = make(map[string]Repository)
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(wire); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func originIdentity(origin string) (string, string, error) {
	var host, path string
	if strings.HasPrefix(origin, "ssh://") || strings.HasPrefix(origin, "https://") {
		rest := origin[strings.Index(origin, "://")+3:]
		if at := strings.IndexByte(rest, '@'); at >= 0 {
			rest = rest[at+1:]
		}
		slash := strings.IndexByte(rest, '/')
		if slash < 0 {
			return "", "", errors.New("origin remote has no repository path")
		}
		host, path = rest[:slash], rest[slash+1:]
	} else if colon := strings.IndexByte(origin, ':'); colon >= 0 {
		rest := origin
		if at := strings.IndexByte(rest, '@'); at >= 0 {
			rest = rest[at+1:]
		}
		colon = strings.IndexByte(rest, ':')
		host, path = rest[:colon], rest[colon+1:]
	} else {
		return "", "", errors.New("origin remote must use SSH or HTTPS")
	}
	if query := strings.IndexByte(path, '?'); query >= 0 {
		path = path[:query]
	}
	if fragment := strings.IndexByte(path, '#'); fragment >= 0 {
		path = path[:fragment]
	}
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	if host == "" || path == "" {
		return "", "", errors.New("origin remote has no repository identity")
	}
	return host, path, nil
}

func pathSlug(name string) string {
	var slug strings.Builder
	dash := false
	for _, character := range strings.ToLower(name) {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			slug.WriteRune(character)
			dash = false
		} else if slug.Len() > 0 && !dash {
			slug.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(slug.String(), "-")
}

func gitCommonDirectory(root string) (string, error) {
	common, err := gitOutput(root, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("cannot resolve Git common directory for worktree: %s", root)
	}
	if common == "" {
		return "", fmt.Errorf("Git common directory is empty for worktree: %s", root)
	}
	if hasUnsupportedControl(common) {
		return "", errors.New("Git common directory contains an unsupported control character")
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(root, common)
	}
	canonical, err := canonicalDirectory(common)
	if err != nil {
		return "", fmt.Errorf("cannot canonicalize Git common directory for worktree %s: %w", root, err)
	}
	return canonical, nil
}

func canonicalDirectory(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", resolved)
	}
	return resolved, nil
}

func gitOutput(dir string, arguments ...string) (string, error) {
	args := append([]string{"-C", dir}, arguments...)
	command := exec.Command("git", args...)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(output), "\n"), nil
}

func hasUnsupportedControl(value string) bool {
	return strings.ContainsAny(value, "\n\r\t")
}
