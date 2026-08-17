package correlation

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProbeExternalRepositoryClassifiesUnavailablePaths(t *testing.T) {
	root := t.TempDir()
	regularFile := filepath.Join(root, "regular-file")
	if err := os.WriteFile(regularFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	notGit := filepath.Join(root, "not-git")
	if err := os.Mkdir(notGit, 0o700); err != nil {
		t.Fatal(err)
	}
	validGit := filepath.Join(root, "valid-git")
	if err := os.Mkdir(validGit, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", validGit, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	tests := []struct {
		name, path, want string
	}{
		{name: "missing", path: filepath.Join(root, "missing"), want: "not_found"},
		{name: "regular file", path: regularFile, want: "not_directory"},
		{name: "directory without git", path: notGit, want: "not_git"},
		{name: "valid checkout", path: validGit, want: ""},
	}
	if runtime.GOOS != "windows" {
		loop := filepath.Join(root, "symlink-loop")
		if err := os.Symlink(loop, loop); err != nil {
			t.Fatal(err)
		}
		tests = append(tests, struct{ name, path, want string }{name: "unreadable metadata", path: loop, want: "unreadable"})
	}

	correlator := NewCorrelator(root, "")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := correlator.probeExternalRepository(test.path)
			if err != nil {
				t.Fatalf("probeExternalRepository: %v", err)
			}
			if got != test.want {
				t.Fatalf("reason = %q, want %q", got, test.want)
			}
		})
	}

}

func TestProbeExternalRepositoryDistinguishesUnreadableAndMalformedMetadata(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-bit behavior is not portable to Windows")
	}

	repository := filepath.Join(t.TempDir(), "repository")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repository, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	headPath := filepath.Join(repository, ".git", "HEAD")
	if err := os.Chmod(headPath, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(headPath, 0o600) })
	if _, err := os.ReadFile(headPath); !os.IsPermission(err) {
		t.Skip("filesystem does not enforce permission bits for this process")
	}

	correlator := NewCorrelator(repository, "")
	if reason, err := correlator.probeExternalRepository(repository); err != nil || reason != "unreadable" {
		t.Fatalf("unreadable Git metadata should be recoverable, got reason=%q err=%v", reason, err)
	}

	if err := os.Chmod(headPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(headPath, []byte("malformed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if reason, err := correlator.probeExternalRepository(repository); err == nil || reason != "" {
		t.Fatalf("readable malformed Git metadata should be fatal, got reason=%q err=%v", reason, err)
	}
}

func TestExpandConfigPathExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := expandConfigPath("~/.config/bv/hub.yaml")
	if err != nil {
		t.Fatalf("expandConfigPath: %v", err)
	}
	want := filepath.Join(home, ".config", "bv", "hub.yaml")
	if got != want {
		t.Fatalf("expandConfigPath = %q, want %q", got, want)
	}
}

func TestHubConfigRepositoriesRequireContextMap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.yaml")
	listConfig := "version: 1\nstore: /tmp/store\nledger: /tmp/ledger\nrepositories:\n  - context: ctx:old\n    path: /tmp/repo\n"
	if err := os.WriteFile(path, []byte(listConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := HubConfigStore(path); err == nil {
		t.Fatal("temporary list repository schema should be rejected")
	}

	mapConfig := "version: 1\nstore: /tmp/store\nledger: /tmp/ledger\nrepositories:\n  z-invalid:\n    path: /tmp/z\n  a-invalid:\n    path: /tmp/a\n"
	if err := os.WriteFile(path, []byte(mapConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := HubConfigStore(path)
	if err == nil || !strings.Contains(err.Error(), `"a-invalid"`) {
		t.Fatalf("expected deterministic first context-key diagnostic, got %v", err)
	}
}

func TestLifecycleEventType(t *testing.T) {
	tests := []struct {
		name              string
		previous, current string
		first             bool
		want              EventType
	}{
		{name: "created", current: "open", first: true, want: EventCreated},
		{name: "claimed", previous: "open", current: "in_progress", want: EventClaimed},
		{name: "closed", previous: "in_progress", current: "closed", want: EventClosed},
		{name: "reopened", previous: "closed", current: "open", want: EventReopened},
		{name: "modified", previous: "open", current: "open", want: EventModified},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := lifecycleEventType(test.previous, test.current, test.first); got != test.want {
				t.Fatalf("lifecycleEventType() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestLoadCorrelationLedgerRejectsMalformedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "correlations.jsonl")
	if err := os.WriteFile(path, []byte("{not-json}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCorrelationLedger(path); err == nil {
		t.Fatal("expected malformed ledger error")
	}
}

func TestLoadHubConfigAllowsEmptyRepositoryMap(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "hub.yaml")
	config := "version: 1\nstore: .beads\nledger: correlations.jsonl\nrepositories: {}\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	hub, err := loadHubConfig(configPath, nil)
	if err != nil {
		t.Fatalf("loadHubConfig: %v", err)
	}
	if len(hub.repositories) != 0 || len(hub.correlations) != 0 {
		t.Fatalf("expected empty external history, got repositories=%v correlations=%v", hub.repositories, hub.correlations)
	}
}

func TestLoadCorrelationLedgerRejectsDanglingSymlink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "correlations.jsonl")
	if err := os.Symlink(filepath.Join(filepath.Dir(path), "missing-target.jsonl"), path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := loadCorrelationLedgerIfExists(path); err == nil {
		t.Fatal("expected dangling ledger symlink to fail as an existing unreadable ledger")
	}
}

func TestFullCommitSHAValidation(t *testing.T) {
	for _, sha := range []string{
		"0123456789abcdef0123456789abcdef01234567",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	} {
		if !fullCommitSHARegex.MatchString(sha) {
			t.Fatalf("expected full object ID to be accepted: %s", sha)
		}
	}
	for _, sha := range []string{"deadbee", "0123456789abcdef0123456789abcdef012345678"} {
		if fullCommitSHARegex.MatchString(sha) {
			t.Fatalf("expected abbreviated/invalid object ID to be rejected: %s", sha)
		}
	}
}
