package correlation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandConfigPathExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := expandConfigPath("~/.config/bv/work-beads.yaml")
	if err != nil {
		t.Fatalf("expandConfigPath: %v", err)
	}
	want := filepath.Join(home, ".config", "bv", "work-beads.yaml")
	if got != want {
		t.Fatalf("expandConfigPath = %q, want %q", got, want)
	}
}

func TestWorkConfigRepositoriesRequireContextMap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "work-beads.yaml")
	listConfig := "version: 1\nstore: /tmp/store\nledger: /tmp/ledger\nrepositories:\n  - context: ctx:old\n    path: /tmp/repo\n"
	if err := os.WriteFile(path, []byte(listConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := WorkConfigStore(path); err == nil {
		t.Fatal("temporary list repository schema should be rejected")
	}

	mapConfig := "version: 1\nstore: /tmp/store\nledger: /tmp/ledger\nrepositories:\n  z-invalid:\n    path: /tmp/z\n  a-invalid:\n    path: /tmp/a\n"
	if err := os.WriteFile(path, []byte(mapConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := WorkConfigStore(path)
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

func TestLoadExternalHistoryManifestAllowsEmptyRepositoryMap(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "work-beads.yaml")
	config := "version: 1\nstore: .beads\nledger: correlations.jsonl\nrepositories: {}\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	manifest, err := loadExternalHistoryManifest(configPath, nil)
	if err != nil {
		t.Fatalf("loadExternalHistoryManifest: %v", err)
	}
	if len(manifest.repositories) != 0 || len(manifest.correlations) != 0 {
		t.Fatalf("expected empty external history, got repositories=%v correlations=%v", manifest.repositories, manifest.correlations)
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
