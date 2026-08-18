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

	"gopkg.in/yaml.v3"
)

// ConfigVersion is the supported Hub configuration schema version.
const ConfigVersion = 1

const changeSignalName = "viewer-generation"

const semanticCacheDirName = "semantic"

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
	config.Repositories[context] = Repository{Path: root}
	if err := writeConfig(paths.Config, config); err != nil {
		return Registration{}, err
	}
	return Registration{Context: context, Root: root}, nil
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
