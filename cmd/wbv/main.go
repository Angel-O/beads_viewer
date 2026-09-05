package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/Dicklesworthstone/beads_viewer/internal/datasource"
	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
	"github.com/Dicklesworthstone/beads_viewer/pkg/loader"
	"golang.org/x/term"
)

type mode string

const (
	modeAuto  mode = "auto"
	modeLocal mode = "local"
	modeHub   mode = "hub"
)

var confidencePattern = regexp.MustCompile(`^(0([.][0-9]+)?|1([.]0+)?)$`)

var sanitizedEnvironment = map[string]bool{
	"BEADS_DIR":                   true,
	"BD_DB":                       true,
	"BEADS_DB":                    true,
	"BD_GLOBAL":                   true,
	"BEADS_DOLT_DATA_DIR":         true,
	"BEADS_DOLT_PORT":             true,
	"BEADS_DOLT_PROXIED_SERVER":   true,
	"BEADS_DOLT_SERVER_DATABASE":  true,
	"BEADS_DOLT_SERVER_HOST":      true,
	"BEADS_DOLT_SERVER_MODE":      true,
	"BEADS_DOLT_SERVER_PORT":      true,
	"BEADS_DOLT_SERVER_SOCKET":    true,
	"BEADS_DOLT_SHARED_SERVER":    true,
	"BV_OUTPUT_FORMAT":            true,
	"TOON_DEFAULT_FORMAT":         true,
	"TOON_STATS":                  true,
	"BV_PRETTY_JSON":              true,
	"BV_ROBOT":                    true,
	"BV_ROBOT_NOT_READY_LABELS":   true,
	"BV_ROBOT_HISTORY_TIMEOUT_MS": true,
	"BV_INSIGHTS_MAP_LIMIT":       true,
	"BV_HUB_CHANGE_SIGNAL":        true,
	"BV_WBV_HUB_SCOPE":            true,
	"BV_WBV_HUB_MODE":             true,
}

type hubScopeRequest struct {
	contexts    []string
	contextless bool
	explicit    bool
}

type runner struct {
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	directory   string
	interactive func() bool
	hubContext  func(string) (string, error)
}

func main() {
	workingDirectory, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wbv: resolving current directory: %v\n", err)
		os.Exit(1)
	}
	r := runner{
		stdin:     os.Stdin,
		stdout:    os.Stdout,
		stderr:    os.Stderr,
		directory: workingDirectory,
		interactive: func() bool {
			return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
		},
	}
	os.Exit(r.run(os.Args[1:]))
}

func (r runner) run(arguments []string) int {
	selectedMode, arguments, err := parseMode(arguments)
	if err != nil {
		return r.die(err)
	}
	if len(arguments) == 1 && (arguments[0] == "--help" || arguments[0] == "-h") {
		r.printHelp()
		return 0
	}
	scopeRequest, arguments, err := parseHubScopeRequest(arguments)
	if err != nil {
		return r.die(err)
	}

	gitRoot := resolveGitRoot(r.directory)
	localMarker := ""
	if gitRoot != "" {
		localMarker = filepath.Join(gitRoot, ".beads")
	}
	localStore := false
	if selectedMode != modeHub && gitRoot != "" {
		localStore, err = validLocalStore(localMarker)
		if err != nil {
			return r.die(err)
		}
	}
	if selectedMode == modeAuto {
		selectedMode = modeHub
		if localStore {
			selectedMode = modeLocal
		}
	}
	if scopeRequest.explicit && selectedMode != modeHub {
		return r.die(errors.New("--context and --contextless are available only with Hub mode"))
	}

	paths := hub.Paths{}
	if selectedMode == modeLocal {
		if gitRoot == "" {
			return r.die(errors.New("local mode requires a Git worktree"))
		}
		if !localStore && !markerExists(localMarker) {
			return r.die(fmt.Errorf("local Beads store is missing at %s", localMarker))
		}
		if !localStore {
			return r.die(fmt.Errorf("local Beads store at %s has no valid issue source", localMarker))
		}
		if !commandExists("bv") {
			return r.die(errors.New("required command not found: bv"))
		}
	} else {
		paths, err = hub.DefaultPaths()
		if err != nil {
			return r.die(err)
		}
		info, statErr := os.Stat(paths.Store)
		if os.IsNotExist(statErr) {
			return r.die(errors.New("store is missing; run 'wbd bootstrap'"))
		}
		if statErr != nil {
			return r.die(fmt.Errorf("accessing Hub store at %s: %w", paths.Store, statErr))
		}
		if !info.IsDir() {
			return r.die(fmt.Errorf("Hub store path is not a directory: %s", paths.Store))
		}
		for _, command := range []string{"bd", "bv", "wbd"} {
			if !commandExists(command) {
				return r.die(fmt.Errorf("required command not found: %s", command))
			}
		}
	}

	viewerArguments, robot, err := parseViewerArguments(arguments)
	if err != nil {
		return r.die(err)
	}
	if !robot && !r.interactive() {
		return r.die(errors.New("bare wbv requires an interactive terminal"))
	}
	if scopeRequest.explicit && !robot {
		return r.die(errors.New("Hub scope options require a Viewer robot invocation"))
	}

	environment := viewerEnvironment(os.Environ())
	viewerDirectory := r.directory
	commandArguments := []string{"--history-mode", "git"}
	if selectedMode == modeLocal {
		viewerDirectory = gitRoot
	} else {
		var hubScope hub.HubScope
		if scopeRequest.explicit {
			hubScope, err = r.resolveHubScope(paths.Config, scopeRequest)
			if err != nil {
				return r.die(err)
			}
		}
		configure := exec.Command("wbd", "configure")
		configure.Dir = r.directory
		configure.Stdin = r.stdin
		configure.Stdout = io.Discard
		configure.Stderr = r.stderr
		if runErr := configure.Run(); runErr != nil {
			if code, ok := childExitCode(runErr); ok {
				return code
			}
			return r.die(fmt.Errorf("running wbd configure: %w", runErr))
		}
		if !scopeRequest.explicit {
			hubScope, err = r.resolveHubScope(paths.Config, scopeRequest)
			if err != nil {
				return r.die(err)
			}
		}
		environment = append(environment, "BEADS_DIR="+paths.Store)
		environment = append(environment, "BV_WBV_HUB_MODE=1")
		if robot {
			scopeJSON, marshalErr := json.Marshal(hubScope)
			if marshalErr != nil {
				return r.die(fmt.Errorf("encoding Hub scope: %w", marshalErr))
			}
			environment = append(environment, "BV_WBV_HUB_SCOPE="+string(scopeJSON))
		}
		if !robot && hubAutoRefreshEnabled(os.Getenv("BV_HUB_AUTO_REFRESH")) {
			environment = append(environment, "BV_HUB_CHANGE_SIGNAL="+hub.ChangeSignalPath(paths))
		}
		commandArguments = []string{"--history-mode", "external", "--hub-config", paths.Config}
	}
	if robot {
		commandArguments = append(commandArguments, viewerArguments...)
		commandArguments = append(commandArguments, "--format", "json")
	}

	viewer := exec.Command("bv", commandArguments...)
	viewer.Dir = viewerDirectory
	viewer.Env = environment
	viewer.Stdin = r.stdin
	viewer.Stdout = r.stdout
	viewer.Stderr = r.stderr
	if runErr := viewer.Run(); runErr != nil {
		if code, ok := childExitCode(runErr); ok {
			return code
		}
		return r.die(fmt.Errorf("running bv: %w", runErr))
	}
	return 0
}

func parseHubScopeRequest(arguments []string) (hubScopeRequest, []string, error) {
	request := hubScopeRequest{}
	remaining := make([]string, 0, len(arguments))
	for i := 0; i < len(arguments); i++ {
		switch arguments[i] {
		case "--context":
			request.explicit = true
			if i+1 >= len(arguments) {
				return hubScopeRequest{}, nil, errors.New("missing value for --context")
			}
			i++
			contextID := arguments[i]
			if err := safeValue("--context", contextID); err != nil {
				return hubScopeRequest{}, nil, err
			}
			request.contexts = append(request.contexts, contextID)
		case "--contextless":
			request.explicit = true
			if request.contextless {
				return hubScopeRequest{}, nil, errors.New("duplicate Viewer option: --contextless")
			}
			request.contextless = true
		default:
			if strings.HasPrefix(arguments[i], "--context=") || strings.HasPrefix(arguments[i], "--contextless=") {
				return hubScopeRequest{}, nil, fmt.Errorf("Hub scope option must not use '=' syntax: %s", strings.SplitN(arguments[i], "=", 2)[0])
			}
			remaining = append(remaining, arguments[i])
		}
	}
	return request, remaining, nil
}

func (r runner) resolveHubScope(configPath string, request hubScopeRequest) (hub.HubScope, error) {
	if request.contextless && len(request.contexts) == 0 {
		return hub.NewContextlessHubScope(), nil
	}
	config, err := hub.Resolve(configPath)
	if len(request.contexts) > 0 {
		if err != nil {
			return hub.HubScope{}, fmt.Errorf("loading registered Hub contexts: %w", err)
		}
		var scope hub.HubScope
		var scopeErr error
		if request.contextless {
			scope, scopeErr = hub.NewSelectedContextsAndContextlessHubScope(request.contexts)
		} else {
			scope, scopeErr = hub.NewSelectedContextsHubScope(request.contexts)
		}
		if scopeErr != nil {
			return hub.HubScope{}, scopeErr
		}
		for _, contextID := range scope.Contexts {
			if _, registered := config.Repositories[contextID]; !registered {
				return hub.HubScope{}, fmt.Errorf("Hub context is not registered: %s", contextID)
			}
		}
		return scope, nil
	}
	if request.explicit {
		return hub.HubScope{}, errors.New("at least one --context value is required")
	}
	if err != nil {
		return hub.NewAllItemsHubScope(), nil
	}
	resolveContext := r.hubContext
	if resolveContext == nil {
		resolveContext = hub.Context
	}
	current, contextErr := resolveContext(r.directory)
	if contextErr != nil {
		return hub.NewAllItemsHubScope(), nil
	}
	if _, registered := config.Repositories[current]; !registered {
		return hub.NewAllItemsHubScope(), nil
	}
	return hub.NewSelectedContextsHubScope([]string{current})
}

func hubAutoRefreshEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func parseMode(arguments []string) (mode, []string, error) {
	selectedMode := modeAuto
	if len(arguments) > 0 && (arguments[0] == "--local" || arguments[0] == "--hub") {
		selectedMode = mode(strings.TrimPrefix(arguments[0], "--"))
		arguments = arguments[1:]
	}
	for _, argument := range arguments {
		switch {
		case argument == "--local" || argument == "--hub":
			return "", nil, fmt.Errorf("mode selector must be the first argument: %s", argument)
		case strings.HasPrefix(argument, "--local=") || strings.HasPrefix(argument, "--hub="):
			return "", nil, fmt.Errorf("mode selector does not take a value: %s", strings.SplitN(argument, "=", 2)[0])
		}
	}
	return selectedMode, arguments, nil
}

func parseViewerArguments(arguments []string) ([]string, bool, error) {
	if len(arguments) == 0 {
		return nil, false, nil
	}
	primary := arguments[0]
	remaining := arguments[1:]
	viewerArguments := []string{primary}
	needsBrief := false

	switch primary {
	case "--robot-plan", "--robot-priority", "--robot-insights", "--robot-graph",
		"--robot-label-health", "--robot-label-flow", "--robot-label-attention",
		"--robot-sprint-list", "--robot-capacity":
	case "--robot-blocker-chain", "--robot-sprint-show", "--robot-forecast":
		if len(remaining) == 0 {
			return nil, false, fmt.Errorf("missing value for %s", primary)
		}
		if err := safeValue(primary, remaining[0]); err != nil {
			return nil, false, err
		}
		viewerArguments = append(viewerArguments, remaining[0])
		remaining = remaining[1:]
	case "--robot-triage":
		needsBrief = true
	default:
		return nil, false, fmt.Errorf("unsupported Viewer invocation: %s", primary)
	}

	seen := make(map[string]bool)
	for len(remaining) > 0 {
		flag := remaining[0]
		remaining = remaining[1:]
		markSeen := func() error {
			if seen[flag] {
				return fmt.Errorf("duplicate Viewer option: %s", flag)
			}
			seen[flag] = true
			return nil
		}
		value := func() (string, error) {
			if len(remaining) == 0 {
				return "", fmt.Errorf("missing value for %s", flag)
			}
			result := remaining[0]
			remaining = remaining[1:]
			return result, nil
		}

		switch flag {
		case "--brief":
			if primary != "--robot-triage" {
				return nil, false, fmt.Errorf("%s is not supported with %s", flag, primary)
			}
			if err := markSeen(); err != nil {
				return nil, false, err
			}
			viewerArguments = append(viewerArguments, flag)
			needsBrief = false
		case "--label":
			if primary != "--robot-plan" && primary != "--robot-priority" && primary != "--robot-insights" {
				return nil, false, fmt.Errorf("%s is not supported with %s", flag, primary)
			}
			option, err := value()
			if err != nil {
				return nil, false, err
			}
			if err := safeValue(flag, option); err != nil {
				return nil, false, err
			}
			if err := markSeen(); err != nil {
				return nil, false, err
			}
			viewerArguments = append(viewerArguments, flag, option)
		case "--robot-min-confidence":
			if primary != "--robot-priority" {
				return nil, false, fmt.Errorf("%s is not supported with %s", flag, primary)
			}
			option, err := value()
			if err != nil {
				return nil, false, err
			}
			if !confidencePattern.MatchString(option) {
				return nil, false, fmt.Errorf("invalid confidence for %s: %s", flag, option)
			}
			if err := markSeen(); err != nil {
				return nil, false, err
			}
			viewerArguments = append(viewerArguments, flag, option)
		case "--robot-max-results":
			if primary != "--robot-priority" {
				return nil, false, fmt.Errorf("%s is not supported with %s", flag, primary)
			}
			option, err := value()
			if err != nil {
				return nil, false, err
			}
			if err := validateUint(flag, option, 1, 1000); err != nil {
				return nil, false, err
			}
			if err := markSeen(); err != nil {
				return nil, false, err
			}
			viewerArguments = append(viewerArguments, flag, option)
		case "--robot-by-label", "--robot-by-assignee":
			if primary != "--robot-priority" {
				return nil, false, fmt.Errorf("%s is not supported with %s", flag, primary)
			}
			option, err := value()
			if err != nil {
				return nil, false, err
			}
			if err := safeValue(flag, option); err != nil {
				return nil, false, err
			}
			if err := markSeen(); err != nil {
				return nil, false, err
			}
			viewerArguments = append(viewerArguments, flag, option)
		case "--graph-format":
			if primary != "--robot-graph" {
				return nil, false, fmt.Errorf("%s is not supported with %s", flag, primary)
			}
			option, err := value()
			if err != nil {
				return nil, false, err
			}
			if option != "json" && option != "dot" && option != "mermaid" {
				return nil, false, fmt.Errorf("invalid graph format: %s", option)
			}
			if err := markSeen(); err != nil {
				return nil, false, err
			}
			viewerArguments = append(viewerArguments, flag, option)
		case "--graph-root":
			if primary != "--robot-graph" {
				return nil, false, fmt.Errorf("%s is not supported with %s", flag, primary)
			}
			option, err := value()
			if err != nil {
				return nil, false, err
			}
			if err := safeValue(flag, option); err != nil {
				return nil, false, err
			}
			if err := markSeen(); err != nil {
				return nil, false, err
			}
			viewerArguments = append(viewerArguments, flag, option)
		case "--graph-depth", "--attention-limit", "--forecast-agents", "--agents":
			allowed := flag == "--graph-depth" && primary == "--robot-graph" ||
				flag == "--attention-limit" && primary == "--robot-label-attention" ||
				flag == "--forecast-agents" && primary == "--robot-forecast" ||
				flag == "--agents" && primary == "--robot-capacity"
			if !allowed {
				return nil, false, fmt.Errorf("%s is not supported with %s", flag, primary)
			}
			option, err := value()
			if err != nil {
				return nil, false, err
			}
			minimum, maximum := uint64(1), uint64(64)
			if flag == "--graph-depth" {
				minimum, maximum = 0, 100
			} else if flag == "--attention-limit" {
				maximum = 1000
			}
			if err := validateUint(flag, option, minimum, maximum); err != nil {
				return nil, false, err
			}
			if err := markSeen(); err != nil {
				return nil, false, err
			}
			viewerArguments = append(viewerArguments, flag, option)
		case "--forecast-label", "--forecast-sprint":
			if primary != "--robot-forecast" {
				return nil, false, fmt.Errorf("%s is not supported with %s", flag, primary)
			}
			option, err := value()
			if err != nil {
				return nil, false, err
			}
			if err := safeValue(flag, option); err != nil {
				return nil, false, err
			}
			if err := markSeen(); err != nil {
				return nil, false, err
			}
			viewerArguments = append(viewerArguments, flag, option)
		case "--capacity-label":
			if primary != "--robot-capacity" {
				return nil, false, fmt.Errorf("%s is not supported with %s", flag, primary)
			}
			option, err := value()
			if err != nil {
				return nil, false, err
			}
			if err := safeValue(flag, option); err != nil {
				return nil, false, err
			}
			if err := markSeen(); err != nil {
				return nil, false, err
			}
			viewerArguments = append(viewerArguments, flag, option)
		case "--robot-not-ready-labels":
			if primary != "--robot-triage" {
				return nil, false, fmt.Errorf("%s is not supported with %s", flag, primary)
			}
			option, err := value()
			if err != nil {
				return nil, false, err
			}
			if err := safeValue(flag, option); err != nil {
				return nil, false, err
			}
			if err := markSeen(); err != nil {
				return nil, false, err
			}
			viewerArguments = append(viewerArguments, flag, option)
		default:
			return nil, false, fmt.Errorf("unsupported Viewer option for %s: %s", primary, flag)
		}
	}
	if needsBrief {
		return nil, false, errors.New("--robot-triage requires --brief")
	}
	return viewerArguments, true, nil
}

func safeValue(flag, value string) error {
	if value == "" {
		return fmt.Errorf("missing value for %s", flag)
	}
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("invalid value for %s: %s", flag, value)
	}
	if strings.ContainsAny(value, "\n\r\t") {
		return fmt.Errorf("invalid control character in %s", flag)
	}
	return nil
}

func validateUint(flag, value string, minimum, maximum uint64) error {
	if value == "" || len(value) > 9 {
		return fmt.Errorf("invalid integer for %s: %s", flag, value)
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return fmt.Errorf("invalid integer for %s: %s", flag, value)
		}
	}
	number, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid integer for %s: %s", flag, value)
	}
	if number < minimum || number > maximum {
		return fmt.Errorf("value for %s must be between %d and %d", flag, minimum, maximum)
	}
	return nil
}

func resolveGitRoot(directory string) string {
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	command.Dir = directory
	command.Stderr = io.Discard
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(output), "\n")
}

func validLocalStore(marker string) (bool, error) {
	info, err := os.Lstat(marker)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspecting local Beads marker at %s: %w", marker, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("local Beads marker must not be a symlink: %s", marker)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("unsupported local Beads marker at %s", marker)
	}

	beadsDir, err := loader.ResolveBeadsDir(marker)
	if err != nil {
		return false, fmt.Errorf("invalid local Beads store at %s: %w", marker, err)
	}
	if _, err := os.ReadDir(beadsDir); err != nil {
		return false, fmt.Errorf("reading local Beads store at %s: %w", marker, err)
	}
	if loader.IsBDWorkspace(beadsDir) {
		return true, nil
	}

	sources, err := datasource.DiscoverSources(datasource.DiscoveryOptions{
		BeadsDir:            beadsDir,
		SkipWorktreeSources: true,
	})
	if err != nil {
		return false, fmt.Errorf("inspecting local Beads store at %s: %w", marker, err)
	}
	var malformed []string
	for _, source := range sources {
		name := filepath.Base(source.Path)
		canonical := name == "beads.db" || preferredJSONLName(name)
		if source.Type == datasource.SourceTypeJSONLLocal {
			valid, validationError := validJSONLIssueSource(source.Path, canonical)
			if valid {
				return true, nil
			}
			if canonical {
				malformed = append(malformed, fmt.Sprintf("%s: %s", name, validationError))
			}
			continue
		}
		if !canonical {
			continue
		}
		if validateErr := datasource.ValidateSource(&source); validateErr == nil {
			return true, nil
		}
		malformed = append(malformed, fmt.Sprintf("%s: %s", name, source.ValidationError))
	}
	if len(malformed) > 0 {
		sort.Strings(malformed)
		return false, fmt.Errorf("local Beads store at %s has no valid issue source (%s)", marker, strings.Join(malformed, "; "))
	}
	return false, nil
}

func validJSONLIssueSource(path string, allowEmpty bool) (bool, string) {
	var stats loader.ParseStats
	if _, err := loader.LoadIssuesFromFileWithOptions(path, loader.ParseOptions{Stats: &stats}); err != nil {
		return false, err.Error()
	}
	maxErrorRate := datasource.DefaultValidationOptions().MaxJSONLErrorRate
	if rate := stats.ErrorRate(); rate > maxErrorRate {
		return false, fmt.Sprintf("too many errors: %.1f%% (max %.1f%%)", rate*100, maxErrorRate*100)
	}
	if stats.Valid == 0 && stats.Errors+stats.Skipped > 0 {
		return false, fmt.Sprintf("no issue records (%d non-issue/error lines)", stats.Errors+stats.Skipped)
	}
	if !allowEmpty && stats.Valid == 0 {
		return false, "no issue records"
	}
	return true, ""
}

func preferredJSONLName(name string) bool {
	for _, preferred := range loader.PreferredJSONLNames {
		if name == preferred {
			return true
		}
	}
	return false
}

func markerExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Lstat(path)
	return err == nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func viewerEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment)+2)
	for _, entry := range environment {
		name := entry
		if equals := strings.IndexByte(entry, '='); equals >= 0 {
			name = entry[:equals]
		}
		if !sanitizedEnvironment[name] && name != "BV_NO_GITIGNORE" && name != "BV_NO_CACHE" {
			result = append(result, entry)
		}
	}
	return append(result, "BV_NO_GITIGNORE=1", "BV_NO_CACHE=1")
}

func childExitCode(err error) (int, bool) {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		if status, ok := exitError.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal()), true
		}
		return exitError.ExitCode(), true
	}
	return 0, false
}

func (r runner) die(err error) int {
	fmt.Fprintf(r.stderr, "wbv: %s\n", err)
	return 1
}

func (r runner) printHelp() {
	fmt.Fprint(r.stdout, `Usage: wbv [--local|--hub] [VIEWER ROBOT INVOCATION]

Without a mode selector, wbv uses the current Git worktree's local .beads
store only when it contains a valid Viewer issue source. Otherwise wbv uses the
private Hub. A linked worktree does not inherit .beads data from another
worktree. Unsafe or malformed local stores fail with an error instead of
silently selecting a mode.

Interactive Hub mode refreshes automatically after successful wbd mutations.
Set BV_HUB_AUTO_REFRESH=0 to disable automatic Hub refresh; Ctrl+R/F5 remains
available for explicit refresh.

  --local  Require a valid local store and use Git history.
  --hub    Always use the private Hub and external history.
  --context <registered-context>
           Select a registered Hub context for robot candidates (repeatable).
  --contextless
           Also select Hub items without any ctx-prefixed label for robot candidates.
  -h, --help
           Show this help.
`)
}
