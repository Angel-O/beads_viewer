package hubmigration_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHubPrefixMigrationIsRepeatableAndRewritesOnlyTopLevelIDs(t *testing.T) {
	h := newHarness(t)
	h.createHub("bead")
	h.write(".local/share/beads/hub/correlations.jsonl", `{"bead_id":"bead-1","nested":{"bead_id":"bead-2"}}
{"bead_id":"other-1"}
`)
	h.write(".local/share/beads/hub/.beads/interactions.jsonl", `{"issue_id":"bead-3","nested":{"issue_id":"bead-4"}}
`)
	h.write(".local/share/beads/hub/.beads/last-touched", "bead-5\n")

	out, err := h.run("migrate-beads-hub-prefix.sh", "task\n")
	if err != nil {
		t.Fatalf("migrate prefix: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Migrated Hub prefix from bead-* to task-*") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	h.wantFile(".local/share/beads/hub/correlations.jsonl", `{"bead_id":"task-1","nested":{"bead_id":"bead-2"}}
{"bead_id":"other-1"}
`)
	h.wantFile(".local/share/beads/hub/.beads/interactions.jsonl", `{"issue_id":"task-3","nested":{"issue_id":"bead-4"}}
`)
	h.wantFile(".local/share/beads/hub/.beads/last-touched", "task-5\n")
	h.wantFile(".local/share/beads/hub/.beads/issue-prefix", "task\n")

	backups, err := filepath.Glob(filepath.Join(h.home, ".local/share/beads/hub-prefix-backup-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backup directories = %v, err = %v", backups, err)
	}
	h.wantAbsoluteFile(filepath.Join(backups[0], "hub/correlations.jsonl"), `{"bead_id":"bead-1","nested":{"bead_id":"bead-2"}}
{"bead_id":"other-1"}
`)

	out, err = h.run("migrate-beads-hub-prefix.sh", "\n")
	if err != nil {
		t.Fatalf("repeat migration no-op: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Hub prefix is already task; no changes made.") {
		t.Fatalf("unexpected no-op output:\n%s", out)
	}
	backups, _ = filepath.Glob(filepath.Join(h.home, ".local/share/beads/hub-prefix-backup-*"))
	if len(backups) != 1 {
		t.Fatalf("no-op created a backup: %v", backups)
	}
	if got := h.mutationCalls(); got != 2 {
		t.Fatalf("mutation calls = %d, want rename plus export only", got)
	}
}

func TestWorkToHubMigrationPreservesRegistrationAndCreatesBackup(t *testing.T) {
	h := newHarness(t)
	oldStore := filepath.Join(h.home, ".local/share/beads/work/.beads")
	oldLedger := filepath.Join(h.home, ".local/share/beads/work/correlations.jsonl")
	h.mkdir(".local/share/beads/work/.beads")
	h.mkdir(".config/bv")
	h.write(".local/share/beads/work/.beads/issue-prefix", "work\n")
	h.write(".local/share/beads/work/correlations.jsonl", "{\"bead_id\":\"work-1\"}\n")
	h.write(".config/bv/work-beads.yaml", fmt.Sprintf(`{"version":1,"store":%q,"ledger":%q,"repositories":{"ctx:project":{"path":"/source/project"}}}
`, oldStore, oldLedger))

	out, err := h.run("migrate-beads-work-to-hub.sh", "\n")
	if err != nil {
		t.Fatalf("migrate work store: %v\n%s", err, out)
	}
	h.wantFile(".local/share/beads/hub/correlations.jsonl", "{\"bead_id\":\"bead-1\"}\n")
	h.wantFile(".local/share/beads/hub/.beads/issue-prefix", "bead\n")
	config := h.read(".config/bv/hub.yaml")
	for _, want := range []string{
		`"version": 1`,
		filepath.Join(h.home, ".local/share/beads/hub/.beads"),
		filepath.Join(h.home, ".local/share/beads/hub/correlations.jsonl"),
		`"ctx:project"`,
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("migrated config missing %q:\n%s", want, config)
		}
	}
	backups, _ := filepath.Glob(filepath.Join(h.home, ".local/share/beads/work-to-hub-backup-*"))
	if len(backups) != 1 {
		t.Fatalf("backup directories = %v", backups)
	}
	h.wantAbsoluteFile(filepath.Join(backups[0], "work/correlations.jsonl"), "{\"bead_id\":\"work-1\"}\n")
}

func TestHubPrefixMigrationRejectsInvalidRuntimeBeforeMutation(t *testing.T) {
	t.Run("symlinked ledger", func(t *testing.T) {
		h := newHarness(t)
		h.createHub("bead")
		target := filepath.Join(h.home, "outside-ledger.jsonl")
		if err := os.WriteFile(target, []byte("{\"bead_id\":\"bead-1\"}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(h.home, ".local/share/beads/hub/correlations.jsonl")); err != nil {
			t.Fatal(err)
		}

		out, err := h.run("migrate-beads-hub-prefix.sh", "task\n")
		if err == nil || !strings.Contains(out, "runtime path is invalid or symlinked") {
			t.Fatalf("expected symlink rejection, err=%v\n%s", err, out)
		}
		h.wantNoMutationOrBackup("hub-prefix-backup-*")
	})

	t.Run("unsupported config version", func(t *testing.T) {
		h := newHarness(t)
		h.createHub("bead")
		store := filepath.Join(h.home, ".local/share/beads/hub/.beads")
		ledger := filepath.Join(h.home, ".local/share/beads/hub/correlations.jsonl")
		h.write(".config/bv/hub.yaml", fmt.Sprintf(`{"version":2,"store":%q,"ledger":%q,"repositories":{}}
`, store, ledger))

		out, err := h.run("migrate-beads-hub-prefix.sh", "task\n")
		if err == nil || !strings.Contains(out, "does not identify the fixed Hub paths") {
			t.Fatalf("expected config rejection, err=%v\n%s", err, out)
		}
		h.wantNoMutationOrBackup("hub-prefix-backup-*")
	})
}

type harness struct {
	t       *testing.T
	home    string
	bin     string
	scripts string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	for _, command := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Skipf("%s is required: %v", command, err)
		}
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	h := &harness{
		t:       t,
		home:    t.TempDir(),
		bin:     t.TempDir(),
		scripts: filepath.Clean(filepath.Join(filepath.Dir(file), "../..", "scripts")),
	}
	h.writeAbsolute(filepath.Join(h.bin, "bd"), `#!/bin/bash
set -euo pipefail
for name in BD_DB BEADS_DB BD_GLOBAL BEADS_DOLT_DATA_DIR BEADS_DOLT_PORT BEADS_DOLT_PROXIED_SERVER BEADS_DOLT_SERVER_DATABASE BEADS_DOLT_SERVER_HOST BEADS_DOLT_SERVER_MODE BEADS_DOLT_SERVER_PORT BEADS_DOLT_SERVER_SOCKET BEADS_DOLT_SHARED_SERVER; do
  [ -z "${!name+x}" ] || { printf 'leaked environment: %s\n' "$name" >&2; exit 90; }
done
[ "$1" = --db ]
store=$2
[ "$BEADS_DIR" = "$store" ]
shift 2
printf '%s\n' "$*" >>"$HOME/bd-calls"
if [ "$1" = --json ] && [ "$2" = config ] && [ "$3" = get ] && [ "$4" = issue_prefix ]; then
  prefix=$(<"$store/issue-prefix")
  printf '{"key":"issue_prefix","schema_version":1,"value":"%s"}\n' "$prefix"
elif [ "$1" = rename-prefix ]; then
  printf '%s\n' "$2" >"$store/issue-prefix"
elif [ "$1" = export ] && [ "$2" = -o ]; then
  prefix=$(<"$store/issue-prefix")
  printf '{"id":"%s-export"}\n' "$prefix" >"$3"
else
  printf 'unexpected bd arguments: %s\n' "$*" >&2
  exit 91
fi
`)
	if err := os.Chmod(filepath.Join(h.bin, "bd"), 0o700); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) createHub(prefix string) {
	h.t.Helper()
	h.mkdir(".local/share/beads/hub/.beads")
	h.mkdir(".config/bv")
	h.write(".local/share/beads/hub/.beads/issue-prefix", prefix+"\n")
	store := filepath.Join(h.home, ".local/share/beads/hub/.beads")
	ledger := filepath.Join(h.home, ".local/share/beads/hub/correlations.jsonl")
	h.write(".config/bv/hub.yaml", fmt.Sprintf(`{"version":1,"store":%q,"ledger":%q,"repositories":{}}
`, store, ledger))
}

func (h *harness) run(script, input string) (string, error) {
	h.t.Helper()
	cmd := exec.Command("bash", filepath.Join(h.scripts, script))
	cmd.Stdin = strings.NewReader(input)
	cmd.Env = append(os.Environ(),
		"HOME="+h.home,
		"PATH="+h.bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"BD_DB=contaminated", "BEADS_DB=contaminated", "BD_GLOBAL=contaminated",
		"BEADS_DOLT_DATA_DIR=contaminated", "BEADS_DOLT_PORT=contaminated",
		"BEADS_DOLT_PROXIED_SERVER=contaminated", "BEADS_DOLT_SERVER_DATABASE=contaminated",
		"BEADS_DOLT_SERVER_HOST=contaminated", "BEADS_DOLT_SERVER_MODE=contaminated",
		"BEADS_DOLT_SERVER_PORT=contaminated", "BEADS_DOLT_SERVER_SOCKET=contaminated",
		"BEADS_DOLT_SHARED_SERVER=contaminated",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (h *harness) mkdir(path string) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Join(h.home, path), 0o700); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) write(path, contents string) {
	h.t.Helper()
	h.writeAbsolute(filepath.Join(h.home, path), contents)
}

func (h *harness) writeAbsolute(path, contents string) {
	h.t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) read(path string) string {
	h.t.Helper()
	contents, err := os.ReadFile(filepath.Join(h.home, path))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(contents)
}

func (h *harness) wantFile(path, want string) {
	h.t.Helper()
	h.wantAbsoluteFile(filepath.Join(h.home, path), want)
}

func (h *harness) wantAbsoluteFile(path, want string) {
	h.t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		h.t.Fatal(err)
	}
	if got := string(contents); got != want {
		h.t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func (h *harness) mutationCalls() int {
	h.t.Helper()
	contents, err := os.ReadFile(filepath.Join(h.home, "bd-calls"))
	if err != nil {
		h.t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(string(contents), "\n") {
		if strings.HasPrefix(line, "rename-prefix ") || strings.HasPrefix(line, "export ") {
			count++
		}
	}
	return count
}

func (h *harness) wantNoMutationOrBackup(pattern string) {
	h.t.Helper()
	if _, err := os.Stat(filepath.Join(h.home, "bd-calls")); err == nil && h.mutationCalls() != 0 {
		h.t.Fatalf("bd mutation occurred:\n%s", h.read("bd-calls"))
	} else if err != nil && !os.IsNotExist(err) {
		h.t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(h.home, ".local/share/beads", pattern))
	if err != nil {
		h.t.Fatal(err)
	}
	if len(backups) != 0 {
		h.t.Fatalf("backup created before validation: %v", backups)
	}
}
