package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
)

var isolatedVariables = map[string]bool{
	"BD_DB": true, "BEADS_DB": true, "BD_GLOBAL": true,
	"BEADS_DOLT_DATA_DIR": true, "BEADS_DOLT_PORT": true,
	"BEADS_DOLT_PROXIED_SERVER": true, "BEADS_DOLT_SERVER_DATABASE": true,
	"BEADS_DOLT_SERVER_HOST": true, "BEADS_DOLT_SERVER_MODE": true,
	"BEADS_DOLT_SERVER_PORT": true, "BEADS_DOLT_SERVER_SOCKET": true,
	"BEADS_DOLT_SHARED_SERVER": true, "BEADS_DIR": true,
}

type app struct {
	paths  hub.Paths
	dir    string
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func newApp(stdin io.Reader, stdout, stderr io.Writer) (*app, error) {
	paths, err := hub.DefaultPaths()
	if err != nil {
		return nil, err
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolving current directory: %w", err)
	}
	return &app{paths: paths, dir: dir, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

func (a *app) run(arguments []string) int {
	command, err := commandName(arguments)
	if err != nil {
		return a.fail(err)
	}
	if command == "bootstrap" {
		request, parseErr := parse(arguments)
		if parseErr != nil {
			return a.fail(parseErr)
		}
		return a.bootstrap(request.prefix)
	}
	if info, statErr := os.Stat(a.paths.Store); statErr != nil || !info.IsDir() {
		return a.fail(errors.New("store is missing; run 'wbd bootstrap'"))
	}
	if err := need("bd"); err != nil {
		return a.fail(err)
	}
	request, err := parse(arguments)
	if err != nil {
		return a.fail(err)
	}

	switch request.command {
	case "context":
		context, contextErr := hub.Context(a.dir)
		if contextErr != nil {
			return a.fail(contextErr)
		}
		fmt.Fprintln(a.stdout, context)
		return 0
	case "configure":
		registration, configureErr := hub.Configure(a.paths, a.dir)
		if configureErr != nil {
			return a.fail(configureErr)
		}
		a.signalRegistration(registration)
		fmt.Fprintln(a.stdout, a.paths.Config)
		return 0
	case "register":
		registration, registerErr := a.register()
		if registerErr != nil {
			return a.fail(registerErr)
		}
		fmt.Fprintf(a.stdout, "%s\t%s\n", registration.Context, registration.Root)
		return 0
	case "create", "new":
		registration, registerErr := a.register()
		if registerErr != nil {
			return a.fail(registerErr)
		}
		args := appendJSON(nil, request.json)
		args = append(args, request.command, "--labels", registration.Context, request.positionals[0])
		args = append(args, request.args...)
		return a.runBDMutation(a.dir, args...)
	case "list":
		args := appendJSON(nil, request.json)
		args = append(args, "list")
		if !request.allContexts {
			registration, registerErr := a.register()
			if registerErr != nil {
				return a.fail(registerErr)
			}
			args = append(args, "--label", registration.Context)
		}
		args = append(args, request.args...)
		return a.runBD(a.dir, args...)
	case "show":
		args := appendJSON(nil, request.json)
		args = append(args, "show", request.positionals[0])
		return a.runBD(a.dir, args...)
	case "update":
		args := appendJSON(nil, request.json)
		args = append(args, "update", request.positionals[0])
		args = append(args, request.args...)
		return a.runBDMutation(a.dir, args...)
	case "dep":
		args := appendJSON(nil, request.json)
		args = append(args, "dep", request.subcommand)
		args = append(args, request.positionals...)
		args = append(args, request.args...)
		return a.runBDMutation(a.dir, args...)
	case "close", "reopen":
		args := appendJSON(nil, request.json)
		args = append(args, request.command, request.positionals[0])
		args = append(args, request.args...)
		return a.runBDMutation(a.dir, args...)
	case "link":
		if err := need("bv"); err != nil {
			return a.fail(err)
		}
		registration, registerErr := a.register()
		if registerErr != nil {
			return a.fail(registerErr)
		}
		commit := "HEAD"
		if len(request.positionals) == 2 {
			commit = request.positionals[1]
		}
		return a.runBV("correlate", "add", "--bead", request.positionals[0], "--repo", registration.Context, "--commit", commit, "--hub-config", a.paths.Config)
	default:
		return a.fail(errors.New("internal unsupported command"))
	}
}

func (a *app) register() (hub.Registration, error) {
	registration, err := hub.Register(a.paths, a.dir)
	if err == nil {
		a.signalRegistration(&registration)
	}
	return registration, err
}

func (a *app) signalRegistration(registration *hub.Registration) {
	if registration == nil || !registration.Changed {
		return
	}
	if err := hub.SignalChange(a.paths); err != nil {
		fmt.Fprintf(a.stderr, "wbd: warning: registry mutation succeeded but Viewer notification failed: %v\n", err)
	}
}

func (a *app) bootstrap(prefix string) int {
	if err := need("bd"); err != nil {
		return a.fail(err)
	}
	if err := need("git"); err != nil {
		return a.fail(err)
	}
	if _, err := os.Stat(a.paths.Store); err == nil || !errors.Is(err, os.ErrNotExist) {
		return a.fail(fmt.Errorf("store already exists: %s", a.paths.Store))
	}
	home := os.Getenv("HOME")
	info, err := os.Stat(home)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o222 == 0 {
		return a.fail(fmt.Errorf("HOME is not writable: %s", home))
	}
	parent := filepath.Dir(a.paths.Store)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return a.fail(fmt.Errorf("cannot create store parent: %s", parent))
	}
	info, err = os.Stat(parent)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o222 == 0 {
		return a.fail(fmt.Errorf("store parent is not writable: %s", parent))
	}
	if commandSucceeded(parent, "git", "rev-parse", "--git-dir") {
		return a.fail(fmt.Errorf("store parent must be outside every Git repository: %s", parent))
	}

	steps := []struct {
		bootstrap bool
		args      []string
	}{
		{true, []string{"metrics", "off"}},
		{true, []string{"init", "--prefix", prefix, "--non-interactive", "--skip-hooks", "--skip-agents"}},
		{false, []string{"config", "set", "export.auto", "false"}},
		{false, []string{"config", "set", "export.git-add", "false"}},
		{false, []string{"config", "set", "dolt.auto-push", "false"}},
	}
	for _, step := range steps {
		code := a.runBDAt(parent, step.bootstrap, step.args...)
		if code != 0 {
			_ = os.RemoveAll(a.paths.Store)
			return code
		}
	}
	if _, err := hub.EnsureConfig(a.paths); err != nil {
		_ = os.RemoveAll(a.paths.Store)
		return a.fail(err)
	}
	return 0
}

func (a *app) runBD(directory string, arguments ...string) int {
	return a.runBDAt(directory, false, arguments...)
}

func (a *app) runBDMutation(directory string, arguments ...string) int {
	code := a.runBD(directory, arguments...)
	if code != 0 {
		return code
	}
	if err := hub.SignalChange(a.paths); err != nil {
		fmt.Fprintf(a.stderr, "wbd: warning: mutation succeeded but Viewer notification failed: %v\n", err)
	}
	return 0
}

func (a *app) runBDAt(directory string, bootstrap bool, arguments ...string) int {
	if !bootstrap {
		arguments = append([]string{"--db", a.paths.Store}, arguments...)
	}
	return a.runChild(directory, "bd", arguments, false)
}

func (a *app) runBV(arguments ...string) int {
	return a.runChild(a.dir, "bv", arguments, true)
}

func (a *app) runChild(directory, name string, arguments []string, viewer bool) int {
	command := exec.Command(name, arguments...)
	command.Dir = directory
	command.Stdin = a.stdin
	command.Stdout = a.stdout
	command.Stderr = a.stderr
	command.Env = isolatedEnvironment(a.paths.Store, viewer)
	err := command.Run()
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		if status, ok := exitError.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
		return exitError.ExitCode()
	}
	return a.fail(fmt.Errorf("running %s: %w", name, err))
}

func (a *app) fail(err error) int {
	fmt.Fprintf(a.stderr, "wbd: %v\n", err)
	return 1
}

func isolatedEnvironment(store string, viewer bool) []string {
	environment := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !isolatedVariables[name] && !(viewer && name == "BV_NO_GITIGNORE") {
			environment = append(environment, entry)
		}
	}
	environment = append(environment, "BEADS_DIR="+store)
	if viewer {
		environment = append(environment, "BV_NO_GITIGNORE=1")
	}
	return environment
}

func need(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("required command not found: %s", name)
	}
	return nil
}

func commandSucceeded(directory, name string, arguments ...string) bool {
	command := exec.Command(name, arguments...)
	command.Dir = directory
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run() == nil
}

func appendJSON(arguments []string, json bool) []string {
	if json {
		return append(arguments, "--json")
	}
	return arguments
}
