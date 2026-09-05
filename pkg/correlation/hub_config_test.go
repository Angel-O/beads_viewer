package correlation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	json "github.com/goccy/go-json"
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

func TestHubConfigStoreDoesNotResolveUnusedLedger(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "hub.yaml")
	config := "version: 1\nstore: .beads\nledger: ~other/ledger.jsonl\nrepositories: {}\n"
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := HubConfigStore(path)
	if err != nil {
		t.Fatalf("HubConfigStore() resolved unused ledger: %v", err)
	}
	if want := filepath.Join(root, ".beads"); store != want {
		t.Fatalf("HubConfigStore() = %q, want %q", store, want)
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

func TestLifecycleEventsFromSnapshotsSkipsUnchangedRows(t *testing.T) {
	snapshots := []beadsHistorySnapshot{
		{
			CommitHash: "unrelated-later",
			Committer:  "root",
			CommitDate: "2026-08-18T00:02:00Z",
			Issue:      json.RawMessage(`{"id":"global-1","title":"Updated","status":"open","priority":1,"updated_at":"2026-08-18T00:01:00Z"}`),
		},
		{
			CommitHash: "created",
			Committer:  "root",
			CommitDate: "2026-08-18T00:00:00Z",
			Issue:      json.RawMessage(`{"id":"global-1","title":"Original","status":"open","priority":2,"updated_at":"2026-08-18T00:00:00Z"}`),
		},
		{
			CommitHash: "modified",
			Committer:  "root",
			CommitDate: "2026-08-18T00:01:00Z",
			Issue:      json.RawMessage(`{"id":"global-1","title":"Updated","status":"open","priority":1,"updated_at":"2026-08-18T00:01:00Z"}`),
		},
	}

	events, err := lifecycleEventsFromSnapshots("global-1", snapshots, CorrelatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2: %#v", len(events), events)
	}
	if events[0].EventType != EventCreated || events[0].CommitSHA != "created" {
		t.Fatalf("first event = %#v, want created snapshot", events[0])
	}
	if events[1].EventType != EventModified || events[1].CommitSHA != "modified" {
		t.Fatalf("second event = %#v, want genuine modification", events[1])
	}
}

func TestLifecycleEventsFromSnapshotsPreservesStatusTransitions(t *testing.T) {
	snapshots := []beadsHistorySnapshot{
		{CommitHash: "created", CommitDate: "2026-08-18T00:00:00Z", Issue: json.RawMessage(`{"id":"global-1","status":"open"}`)},
		{CommitHash: "claimed", CommitDate: "2026-08-18T00:01:00Z", Issue: json.RawMessage(`{"id":"global-1","status":"in_progress"}`)},
		{CommitHash: "closed", CommitDate: "2026-08-18T00:02:00Z", Issue: json.RawMessage(`{"id":"global-1","status":"closed"}`)},
	}

	events, err := lifecycleEventsFromSnapshots("global-1", snapshots, CorrelatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []EventType{EventCreated, EventClaimed, EventClosed}
	if len(events) != len(want) {
		t.Fatalf("events = %d, want %d", len(events), len(want))
	}
	for i, eventType := range want {
		if events[i].EventType != eventType {
			t.Fatalf("event %d type = %q, want %q", i, events[i].EventType, eventType)
		}
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

func TestHistoryLoadSkipsUnknownLedgerRecordsAndRetainsValidCorrelations(t *testing.T) {
	root := t.TempDir()
	repository := filepath.Join(root, "repository")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repository, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(repository, "file.go"), []byte("package fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"config", "user.name", "History Test"},
		{"config", "user.email", "history@example.invalid"},
		{"add", "file.go"},
		{"commit", "--quiet", "-m", "valid correlation"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repository}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	validSHAOutput, err := exec.Command("git", "-C", repository, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse: %v\n%s", err, validSHAOutput)
	}
	validSHA := strings.TrimSpace(string(validSHAOutput))

	ledgerPath := filepath.Join(root, "correlations.jsonl")
	ledger := fmt.Sprintf("%s\n%s\n",
		`{"bead_id":"stale-issue","context":"ctx:source","commit":"`+strings.Repeat("0", 40)+`"}`,
		`{"bead_id":"known-issue","context":"ctx:source","commit":"`+validSHA+`"}`)
	if err := os.WriteFile(ledgerPath, []byte(ledger), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "hub.yaml")
	config := fmt.Sprintf("version: 1\nstore: %s\nledger: %s\nrepositories:\n  ctx:source:\n    path: %s\n", filepath.Join(root, "store"), ledgerPath, repository)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	bd := `#!/bin/sh
printf '%s\n' '{"schema_version":1,"issues":[{"issue_id":"known-issue","snapshots":[]}]}'
`
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(bd), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	correlator := NewCorrelator(repository).WithContext(context.Background())
	correlator.hubConfigPath = configPath
	report, err := correlator.GenerateReport([]BeadInfo{{ID: "known-issue", Labels: []string{"ctx:source"}}}, CorrelatorOptions{})
	if err != nil {
		t.Fatalf("GenerateReport: %v", err)
	}
	commits := report.Histories["known-issue"].Commits
	if len(commits) != 1 || commits[0].SHA != validSHA {
		t.Fatalf("valid ledger correlation = %#v, want commit %q", commits, validSHA)
	}
	if data, err := os.ReadFile(ledgerPath); err != nil || string(data) != ledger {
		t.Fatalf("history load changed the ledger: data=%q err=%v", data, err)
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

func TestRemoveExternalCorrelationRemovesExactDuplicatesAndPreservesUnrelatedRawRecords(t *testing.T) {
	configPath, ledgerPath := correlationRemovalFixture(t)
	sha := "0123456789abcdef0123456789abcdef01234567"
	otherSHA := "89abcdef0123456789abcdef0123456789abcdef"
	unrelatedFirst := ` {"bead_id":"item-beta","context":"ctx:synthetic-a","commit":"` + sha + `","extra":true}`
	unrelatedSecond := `{"bead_id":"item-alpha","context":"ctx:synthetic-a","commit":"` + otherSHA + `"}`
	ledger := unrelatedFirst + "\n" +
		`{"bead_id":"item-alpha","context":"ctx:synthetic-a","commit":"` + sha + `"}` + "\n" +
		unrelatedSecond + "\n" +
		`{"bead_id":"item-alpha","context":"ctx:synthetic-a","commit":"0123456789ABCDEF0123456789ABCDEF01234567"}` + "\n"
	if err := os.WriteFile(ledgerPath, []byte(ledger), 0o600); err != nil {
		t.Fatal(err)
	}

	record, removed, err := RemoveExternalCorrelation(configPath, "item-alpha", "ctx:synthetic-a", sha)
	if err != nil {
		t.Fatalf("RemoveExternalCorrelation: %v", err)
	}
	if !removed || record.BeadID != "item-alpha" || record.Context != "ctx:synthetic-a" || record.Commit != sha {
		t.Fatalf("result = %#v, removed=%v", record, removed)
	}
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := unrelatedFirst + "\n" + unrelatedSecond + "\n"; string(data) != want {
		t.Fatalf("ledger = %q, want preserved unrelated records %q", data, want)
	}
}

func TestRemoveExternalCorrelationWrongTupleAndNotFoundAreIdempotent(t *testing.T) {
	configPath, ledgerPath := correlationRemovalFixture(t)
	sha := "0123456789abcdef0123456789abcdef01234567"
	otherSHA := "89abcdef0123456789abcdef0123456789abcdef"
	original := `{"bead_id":"item-alpha","context":"ctx:synthetic-a","commit":"` + sha + `"}` + "\n"
	if err := os.WriteFile(ledgerPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	record, removed, err := RemoveExternalCorrelation(configPath, "item-alpha", "ctx:synthetic-a", otherSHA)
	if err != nil {
		t.Fatalf("wrong tuple: %v", err)
	}
	if removed || record.Commit != otherSHA {
		t.Fatalf("wrong tuple result = %#v, removed=%v", record, removed)
	}
	data, err := os.ReadFile(ledgerPath)
	if err != nil || string(data) != original {
		t.Fatalf("wrong tuple changed ledger: data=%q err=%v", data, err)
	}

	missingConfigPath, missingLedgerPath := correlationRemovalFixture(t)
	record, removed, err = RemoveExternalCorrelation(missingConfigPath, "item-alpha", "ctx:synthetic-a", sha)
	if err != nil || removed || record.Commit != sha {
		t.Fatalf("missing ledger result = %#v, removed=%v, err=%v", record, removed, err)
	}
	if _, err := os.Stat(missingLedgerPath); !os.IsNotExist(err) {
		t.Fatalf("not-found removal created ledger: %v", err)
	}
}

func TestRemoveExternalCorrelationRejectsNonFullSHA(t *testing.T) {
	configPath, ledgerPath := correlationRemovalFixture(t)
	if _, _, err := RemoveExternalCorrelation(configPath, "item-alpha", "ctx:synthetic-a", "0123456"); err == nil || !strings.Contains(err.Error(), "full 40- or 64-character") {
		t.Fatalf("abbreviated SHA error = %v", err)
	}
	if _, err := os.Stat(ledgerPath); !os.IsNotExist(err) {
		t.Fatalf("invalid removal touched ledger: %v", err)
	}
}

func TestRemoveExternalCorrelationRejectsUnauthorizedBeadContext(t *testing.T) {
	configPath, ledgerPath := correlationRemovalFixture(t)
	sha := "0123456789abcdef0123456789abcdef01234567"
	original := `{"bead_id":"item-alpha","context":"ctx:synthetic-a","commit":"` + sha + `"}` + "\n"
	if err := os.WriteFile(ledgerPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	repositoryB := filepath.Join(filepath.Dir(configPath), "repository-b")
	if err := os.Mkdir(repositoryB, 0o700); err != nil {
		t.Fatal(err)
	}
	config, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := config.WriteString("  ctx:synthetic-b:\n    path: " + repositoryB + "\n")
	closeErr := config.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("extending config: write=%v close=%v", writeErr, closeErr)
	}

	if _, _, err := RemoveExternalCorrelation(configPath, "item-alpha", "ctx:synthetic-b", sha); err == nil || !strings.Contains(err.Error(), "does not carry context label") {
		t.Fatalf("unauthorized context error = %v", err)
	}
	if data, err := os.ReadFile(ledgerPath); err != nil || string(data) != original {
		t.Fatalf("unauthorized context changed ledger: data=%q err=%v", data, err)
	}
}

func TestRemoveExternalCorrelationMalformedLedgerAndWriteFailurePreserveLedger(t *testing.T) {
	t.Run("malformed ledger", func(t *testing.T) {
		configPath, ledgerPath := correlationRemovalFixture(t)
		original := []byte("{not-json}\n")
		if err := os.WriteFile(ledgerPath, original, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := RemoveExternalCorrelation(configPath, "item-alpha", "ctx:synthetic-a", "0123456789abcdef0123456789abcdef01234567"); err == nil {
			t.Fatal("malformed ledger removal succeeded")
		}
		if data, err := os.ReadFile(ledgerPath); err != nil || string(data) != string(original) {
			t.Fatalf("malformed ledger changed: data=%q err=%v", data, err)
		}
	})

	t.Run("atomic write failure", func(t *testing.T) {
		configPath, ledgerPath := correlationRemovalFixture(t)
		original := []byte(`{"bead_id":"item-alpha","context":"ctx:synthetic-a","commit":"0123456789abcdef0123456789abcdef01234567"}` + "\n")
		if err := os.WriteFile(ledgerPath, original, 0o600); err != nil {
			t.Fatal(err)
		}
		failingWriter := func(string, []correlationLedgerEntry) (bool, error) {
			return false, errors.New("synthetic write failure")
		}
		if _, _, err := removeExternalCorrelation(configPath, "item-alpha", "ctx:synthetic-a", "0123456789abcdef0123456789abcdef01234567", failingWriter); err == nil || !strings.Contains(err.Error(), "synthetic write failure") {
			t.Fatalf("write failure error = %v", err)
		}
		if data, err := os.ReadFile(ledgerPath); err != nil || string(data) != string(original) {
			t.Fatalf("failed write changed ledger: data=%q err=%v", data, err)
		}
	})
}

func TestCorrelationPostRenameFailureReportsCommittedMutation(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	postRenameFailure := func(path string, entries []correlationLedgerEntry) (bool, error) {
		committed, err := writeCorrelationLedgerAtomic(path, entries)
		if err != nil {
			return committed, err
		}
		return true, errors.New("synthetic post-rename durability failure")
	}

	t.Run("remove", func(t *testing.T) {
		configPath, ledgerPath := correlationRemovalFixture(t)
		original := `{"bead_id":"item-alpha","context":"ctx:synthetic-a","commit":"` + sha + `"}` + "\n"
		if err := os.WriteFile(ledgerPath, []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}

		record, removed, err := removeExternalCorrelation(configPath, "item-alpha", "ctx:synthetic-a", sha, postRenameFailure)
		if !removed || err == nil || !strings.Contains(err.Error(), "post-rename durability failure") {
			t.Fatalf("record=%#v removed=%v err=%v", record, removed, err)
		}
		if record.BeadID != "item-alpha" || record.Context != "ctx:synthetic-a" || record.Commit != sha {
			t.Fatalf("committed removal returned wrong record: %#v", record)
		}
		if data, readErr := os.ReadFile(ledgerPath); readErr != nil || len(data) != 0 {
			t.Fatalf("committed removal ledger=%q err=%v", data, readErr)
		}
	})

	t.Run("add", func(t *testing.T) {
		configPath, ledgerPath := correlationRemovalFixture(t)
		repository := filepath.Join(filepath.Dir(configPath), "repository")
		for _, arguments := range [][]string{
			{"init", "--quiet"},
			{"config", "user.name", "Synthetic Test"},
			{"config", "user.email", "synthetic@example.invalid"},
			{"commit", "--allow-empty", "--quiet", "-m", "synthetic correlation fixture"},
		} {
			if out, commandErr := exec.Command("git", append([]string{"-C", repository}, arguments...)...).CombinedOutput(); commandErr != nil {
				t.Fatalf("git %v: %v\n%s", arguments, commandErr, out)
			}
		}

		record, added, err := addExternalCorrelation(configPath, "item-alpha", "ctx:synthetic-a", "HEAD", postRenameFailure)
		if !added || err == nil || !strings.Contains(err.Error(), "post-rename durability failure") {
			t.Fatalf("record=%#v added=%v err=%v", record, added, err)
		}
		data, readErr := os.ReadFile(ledgerPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		entries, loadErr := loadCorrelationLedger(ledgerPath)
		if loadErr != nil || len(entries) != 1 || entries[0] != record {
			t.Fatalf("committed add ledger=%q entries=%#v err=%v", data, entries, loadErr)
		}
	})
}

func TestRemoveExternalCorrelationRejectsInvalidUnrelatedRecordsWithoutMutation(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	otherSHA := "89abcdef0123456789abcdef0123456789abcdef"
	validTarget := `{"bead_id":"item-alpha","context":"ctx:synthetic-a","commit":"` + sha + `"}`
	tests := []struct {
		name    string
		records []string
		want    string
	}{
		{name: "empty field", records: []string{validTarget, `{"bead_id":"item-beta","context":"","commit":"` + otherSHA + `"}`}, want: "requires non-empty"},
		{name: "invalid full SHA", records: []string{validTarget, `{"bead_id":"item-beta","context":"ctx:synthetic-a","commit":"89abcdef"}`}, want: "must be a full"},
		{name: "undefined context", records: []string{validTarget, `{"bead_id":"item-beta","context":"ctx:undefined","commit":"` + otherSHA + `"}`}, want: "undefined context"},
		{name: "mismatched context", records: []string{validTarget, `{"bead_id":"item-mismatch","context":"ctx:synthetic-a","commit":"` + otherSHA + `"}`}, want: "does not carry context label"},
		{name: "unknown bead", records: []string{validTarget, `{"bead_id":"item-missing","context":"ctx:synthetic-a","commit":"` + otherSHA + `"}`}, want: "was not found"},
		{name: "ineligible bead", records: []string{validTarget, `{"bead_id":"item-todo","context":"ctx:synthetic-a","commit":"` + otherSHA + `"}`}, want: "cannot be correlated"},
		{name: "duplicate unrelated tuple", records: []string{validTarget, `{"bead_id":"item-beta","context":"ctx:synthetic-a","commit":"` + otherSHA + `"}`, `{"bead_id":"item-beta","context":"ctx:synthetic-a","commit":"` + otherSHA + `"}`}, want: "repeats correlation"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			configPath, ledgerPath := correlationRemovalFixture(t)
			original := strings.Join(testCase.records, "\n") + "\n"
			if err := os.WriteFile(ledgerPath, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			writerCalled := false
			writer := func(string, []correlationLedgerEntry) (bool, error) {
				writerCalled = true
				return true, nil
			}
			if _, _, err := removeExternalCorrelation(configPath, "item-alpha", "ctx:synthetic-a", sha, writer); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %v, want containing %q", err, testCase.want)
			}
			if writerCalled {
				t.Fatal("invalid unrelated record invoked ledger writer")
			}
			if data, err := os.ReadFile(ledgerPath); err != nil || string(data) != original {
				t.Fatalf("invalid unrelated record changed ledger: data=%q err=%v", data, err)
			}
		})
	}
}

func correlationRemovalFixture(t *testing.T) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX fake bd executable")
	}
	root := t.TempDir()
	store := filepath.Join(root, "store")
	repository := filepath.Join(root, "repository")
	bin := filepath.Join(root, "bin")
	for _, path := range []string{store, repository, bin} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	bd := `#!/bin/sh
bead="$5"
case "$bead" in
  item-alpha|item-beta) printf '[{"id":"%s","issue_type":"task","labels":["ctx:synthetic-a"]}]\n' "$bead" ;;
  item-mismatch) printf '[{"id":"%s","issue_type":"task","labels":["ctx:synthetic-b"]}]\n' "$bead" ;;
  item-todo) printf '[{"id":"%s","issue_type":"todo","labels":["ctx:synthetic-a"]}]\n' "$bead" ;;
  *) printf '[]\n' ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(bd), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ledgerPath := filepath.Join(root, "private", "correlations.jsonl")
	if err := os.MkdirAll(filepath.Dir(ledgerPath), 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "hub.yaml")
	config := "version: 1\nstore: " + store + "\nledger: " + ledgerPath + "\nrepositories:\n  ctx:synthetic-a:\n    path: " + repository + "\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath, ledgerPath
}
