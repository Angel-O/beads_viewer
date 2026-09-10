package hub

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
)

func TestExternalHistorySourceLoadsValidatedSnapshot(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "private", "correlations.jsonl")
	config := filepath.Join(root, "hub.yaml")
	data := "version: 1\nstore: store\nledger: private/correlations.jsonl\nrepositories:\n  ctx:source:\n    path: source\n"
	if err := os.MkdirAll(filepath.Join(root, "source"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(ledger), 0o700); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	if err := os.WriteFile(ledger, []byte(`{"bead_id":"item-1","context":"ctx:source","commit":"`+sha+`"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewExternalHistorySource(config)([]correlation.BeadInfo{{ID: "item-1", Labels: []string{"ctx:source"}}})
	if err != nil {
		t.Fatalf("load external snapshot: %v", err)
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("snapshot validation: %v", err)
	}
	if snapshot.Store != filepath.Join(root, "store") || snapshot.Ledger != ledger || len(snapshot.Correlations) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestExternalHistorySourceAllowsEmptyRepositoryMap(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "hub.yaml")
	data := "version: 1\nstore: .beads\nledger: correlations.jsonl\nrepositories: {}\n"
	if err := os.WriteFile(config, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewExternalHistorySource(config)(nil)
	if err != nil {
		t.Fatalf("load external snapshot: %v", err)
	}
	if snapshot.Store != filepath.Join(root, ".beads") || snapshot.Ledger != filepath.Join(root, "correlations.jsonl") || len(snapshot.Correlations) != 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestExternalHistorySourceSkipsStaleRecordsAndPreservesLedger(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "correlations.jsonl")
	config := filepath.Join(root, "hub.yaml")
	sha := strings.Repeat("a", 40)
	original := `{"bead_id":"stale-issue","context":"ctx:source","commit":"` + strings.Repeat("0", 40) + `"}` + "\n" +
		`{"bead_id":"known-issue","context":"ctx:source","commit":"` + sha + `"}` + "\n"
	data := "version: 1\nstore: store\nledger: correlations.jsonl\nrepositories:\n  ctx:source:\n    path: source\n"
	if err := os.WriteFile(config, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledger, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewExternalHistorySource(config)([]correlation.BeadInfo{{ID: "known-issue", Labels: []string{"ctx:source"}}})
	if err != nil {
		t.Fatalf("load external snapshot: %v", err)
	}
	if len(snapshot.Correlations) != 1 || snapshot.Correlations[0].BeadID != "known-issue" {
		t.Fatalf("correlations = %#v", snapshot.Correlations)
	}
	dataAfter, err := os.ReadFile(ledger)
	if err != nil || string(dataAfter) != original {
		t.Fatalf("history load changed ledger: data=%q err=%v", dataAfter, err)
	}
}

func TestExternalHistorySourceRejectsDuplicateKnownRecords(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "correlations.jsonl")
	config := filepath.Join(root, "hub.yaml")
	sha := strings.Repeat("a", 40)
	data := "version: 1\nstore: store\nledger: correlations.jsonl\nrepositories:\n  ctx:source:\n    path: source\n"
	if err := os.WriteFile(config, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	duplicate := `{"bead_id":"known-issue","context":"ctx:source","commit":"` + sha + `"}` + "\n" +
		`{"bead_id":"known-issue","context":"ctx:source","commit":"` + strings.ToUpper(sha) + `"}` + "\n"
	if err := os.WriteFile(ledger, []byte(duplicate), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewExternalHistorySource(config)([]correlation.BeadInfo{{ID: "known-issue", Labels: []string{"ctx:source"}}}); err == nil || !strings.Contains(err.Error(), "repeats correlation") {
		t.Fatalf("duplicate ledger error = %v", err)
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

func TestLoadCorrelationLedgerRejectsDanglingSymlink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "correlations.jsonl")
	if err := os.Symlink(filepath.Join(filepath.Dir(path), "missing-target.jsonl"), path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := loadCorrelationLedgerIfExists(path); err == nil {
		t.Fatal("expected dangling ledger symlink to fail as an existing unreadable ledger")
	}
}

func TestFullExternalCommitSHAValidation(t *testing.T) {
	for _, sha := range []string{
		"0123456789abcdef0123456789abcdef01234567",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	} {
		if !fullExternalCommitSHA.MatchString(sha) {
			t.Fatalf("expected full object ID to be accepted: %s", sha)
		}
	}
	for _, sha := range []string{"deadbee", "0123456789abcdef0123456789abcdef012345678"} {
		if fullExternalCommitSHA.MatchString(sha) {
			t.Fatalf("expected abbreviated/invalid object ID to be rejected: %s", sha)
		}
	}
}

func TestRemoveExternalCorrelationRemovesExactDuplicatesAndPreservesUnrelatedRawRecords(t *testing.T) {
	configPath, ledgerPath := externalHistoryMutationFixture(t)
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
	configPath, ledgerPath := externalHistoryMutationFixture(t)
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

	missingConfigPath, missingLedgerPath := externalHistoryMutationFixture(t)
	record, removed, err = RemoveExternalCorrelation(missingConfigPath, "item-alpha", "ctx:synthetic-a", sha)
	if err != nil || removed || record.Commit != sha {
		t.Fatalf("missing ledger result = %#v, removed=%v, err=%v", record, removed, err)
	}
	if _, err := os.Stat(missingLedgerPath); !os.IsNotExist(err) {
		t.Fatalf("not-found removal created ledger: %v", err)
	}
}

func TestRemoveExternalCorrelationRejectsNonFullSHA(t *testing.T) {
	configPath, ledgerPath := externalHistoryMutationFixture(t)
	if _, _, err := RemoveExternalCorrelation(configPath, "item-alpha", "ctx:synthetic-a", "0123456"); err == nil || !strings.Contains(err.Error(), "full 40- or 64-character") {
		t.Fatalf("abbreviated SHA error = %v", err)
	}
	if _, err := os.Stat(ledgerPath); !os.IsNotExist(err) {
		t.Fatalf("invalid removal touched ledger: %v", err)
	}
}

func TestRemoveExternalCorrelationRejectsUnauthorizedBeadContext(t *testing.T) {
	configPath, ledgerPath := externalHistoryMutationFixture(t)
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
		configPath, ledgerPath := externalHistoryMutationFixture(t)
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
		configPath, ledgerPath := externalHistoryMutationFixture(t)
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

func TestExternalCorrelationPostRenameFailureReportsCommittedMutation(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	postRenameFailure := func(path string, entries []correlationLedgerEntry) (bool, error) {
		committed, err := writeCorrelationLedgerAtomic(path, entries)
		if err != nil {
			return committed, err
		}
		return true, errors.New("synthetic post-rename durability failure")
	}

	t.Run("remove", func(t *testing.T) {
		configPath, ledgerPath := externalHistoryMutationFixture(t)
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
		configPath, ledgerPath := externalHistoryMutationFixture(t)
		repository := filepath.Join(filepath.Dir(configPath), "repository")
		gitRun(t, repository, "init", "--quiet")
		configureGitIdentity(t, repository)
		gitRun(t, repository, "commit", "--allow-empty", "--quiet", "-m", "synthetic correlation fixture")

		record, added, err := addExternalCorrelation(configPath, "item-alpha", "ctx:synthetic-a", "HEAD", postRenameFailure)
		if !added || err == nil || !strings.Contains(err.Error(), "post-rename durability failure") {
			t.Fatalf("record=%#v added=%v err=%v", record, added, err)
		}
		data, readErr := os.ReadFile(ledgerPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		entries, loadErr := loadCorrelationLedger(ledgerPath)
		if loadErr != nil || len(entries) != 1 || entries[0].correlation != record {
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
			configPath, ledgerPath := externalHistoryMutationFixture(t)
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

func externalHistoryMutationFixture(t *testing.T) (string, string) {
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
