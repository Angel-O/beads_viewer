package correlation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const beadsHistoryHelperEnv = "BV_BEADS_HISTORY_HELPER"

func TestBeadsHistoryHelperProcess(t *testing.T) {
	if os.Getenv(beadsHistoryHelperEnv) == "" {
		return
	}
	if err := os.WriteFile(os.Getenv("BV_BEADS_HISTORY_PID"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		os.Exit(91)
	}
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(92)
	}
	if err := os.WriteFile(os.Getenv("BV_BEADS_HISTORY_STDIN"), stdin, 0o600); err != nil {
		os.Exit(93)
	}
	switch os.Getenv("BV_BEADS_HISTORY_MODE") {
	case "failure":
		_, _ = fmt.Fprintln(os.Stderr, os.Getenv("BV_BEADS_HISTORY_STDERR"))
		os.Exit(2)
	case "block":
		time.Sleep(time.Hour)
	case "oversized":
		_, _ = fmt.Fprintln(os.Stderr, os.Getenv("BV_BEADS_HISTORY_STDERR"))
		chunk := strings.Repeat("x", 64<<10)
		for written := 0; written <= beadsBulkHistoryMaxResponseBytes; written += len(chunk) {
			if _, err := os.Stdout.WriteString(chunk); err != nil {
				os.Exit(0)
			}
		}
		time.Sleep(time.Hour)
	default:
		_, _ = os.Stdout.WriteString(os.Getenv("BV_BEADS_HISTORY_RESPONSE"))
		os.Exit(0)
	}
}

type beadsHistoryProcessFixture struct {
	invocations string
	args        string
	stdin       string
	pid         string
}

func installBeadsHistoryHelper(t *testing.T, mode, response, stderr string) beadsHistoryProcessFixture {
	t.Helper()
	if os.PathSeparator == '\\' {
		t.Skip("helper executable fixture requires POSIX executable lookup")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	wrapper := "#!/bin/sh\nprintf '1\\n' >> \"$BV_BEADS_HISTORY_INVOCATIONS\"\n: > \"$BV_BEADS_HISTORY_ARGS\"\nfor arg in \"$@\"; do printf '%s\\0' \"$arg\" >> \"$BV_BEADS_HISTORY_ARGS\"; done\nexec \"$BV_BEADS_HISTORY_TEST_BINARY\" -test.run=^TestBeadsHistoryHelperProcess$\n"
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := beadsHistoryProcessFixture{
		invocations: filepath.Join(dir, "invocations"),
		args:        filepath.Join(dir, "args"),
		stdin:       filepath.Join(dir, "stdin"),
		pid:         filepath.Join(dir, "pid"),
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(beadsHistoryHelperEnv, "1")
	t.Setenv("BV_BEADS_HISTORY_INVOCATIONS", fixture.invocations)
	t.Setenv("BV_BEADS_HISTORY_ARGS", fixture.args)
	t.Setenv("BV_BEADS_HISTORY_STDIN", fixture.stdin)
	t.Setenv("BV_BEADS_HISTORY_PID", fixture.pid)
	t.Setenv("BV_BEADS_HISTORY_TEST_BINARY", os.Args[0])
	t.Setenv("BV_BEADS_HISTORY_MODE", mode)
	t.Setenv("BV_BEADS_HISTORY_RESPONSE", response)
	t.Setenv("BV_BEADS_HISTORY_STDERR", stderr)
	return fixture
}

func TestLoadBeadsLifecycleUsesOneBulkProcessAndPreservesEvents(t *testing.T) {
	response := `{"schema_version":1,"issues":[` +
		`{"issue_id":"alpha","snapshots":[` +
		`{"CommitHash":"alpha-claimed","Committer":"Lifecycle Bot","CommitDate":"2026-01-03T03:04:05Z","Issue":{"id":"alpha","status":"in_progress"}},` +
		`{"CommitHash":"alpha-created","Committer":"Lifecycle Bot","CommitDate":"2026-01-01T03:04:05Z","Issue":{"id":"alpha","status":"open"}}]},` +
		`{"issue_id":"missing","snapshots":[]},` +
		`{"issue_id":"zeta","snapshots":[` +
		`{"CommitHash":"zeta-closed","Committer":"Lifecycle Bot","CommitDate":"2026-01-04T03:04:05Z","Issue":{"id":"zeta","status":"closed"}},` +
		`{"CommitHash":"zeta-created","Committer":"Lifecycle Bot","CommitDate":"2026-01-02T03:04:05Z","Issue":{"id":"zeta","status":"open"}}]}` +
		`]}`
	fixture := installBeadsHistoryHelper(t, "success", response, "")

	beads := []BeadInfo{{ID: "zeta"}, {ID: "missing"}, {ID: "alpha"}}
	got, err := loadBeadsLifecycle(context.Background(), "/fixture/store", beads, CorrelatorOptions{})
	if err != nil {
		t.Fatal(err)
	}

	alpha := []beadsHistorySnapshot{
		{CommitHash: "alpha-claimed", Committer: "Lifecycle Bot", CommitDate: "2026-01-03T03:04:05Z", Issue: json.RawMessage(`{"id":"alpha","status":"in_progress"}`)},
		{CommitHash: "alpha-created", Committer: "Lifecycle Bot", CommitDate: "2026-01-01T03:04:05Z", Issue: json.RawMessage(`{"id":"alpha","status":"open"}`)},
	}
	zeta := []beadsHistorySnapshot{
		{CommitHash: "zeta-closed", Committer: "Lifecycle Bot", CommitDate: "2026-01-04T03:04:05Z", Issue: json.RawMessage(`{"id":"zeta","status":"closed"}`)},
		{CommitHash: "zeta-created", Committer: "Lifecycle Bot", CommitDate: "2026-01-02T03:04:05Z", Issue: json.RawMessage(`{"id":"zeta","status":"open"}`)},
	}
	wantAlpha, err := lifecycleEventsFromSnapshots("alpha", alpha, CorrelatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wantZeta, err := lifecycleEventsFromSnapshots("zeta", zeta, CorrelatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := append(wantAlpha, wantZeta...)
	sort.SliceStable(want, func(i, j int) bool { return want[i].Timestamp.Before(want[j].Timestamp) })
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bulk lifecycle events differ from repeated single-ID semantics:\ngot:  %#v\nwant: %#v", got, want)
	}
	assertBeadsHistoryProcess(t, fixture, bulkHistoryArgs("/fixture/store"), "alpha\nmissing\nzeta\n")
}

func TestLoadBeadsLifecycle76IDsStartsOneProcess(t *testing.T) {
	ids := make([]string, 76)
	groups := make([]string, 76)
	beads := make([]BeadInfo, 76)
	for i := range ids {
		ids[i] = fmt.Sprintf("work-%03d", i)
		groups[i] = fmt.Sprintf(`{"issue_id":%q,"snapshots":[]}`, ids[i])
		beads[75-i] = BeadInfo{ID: ids[i]}
	}
	response := `{"schema_version":1,"issues":[` + strings.Join(groups, ",") + `]}`
	fixture := installBeadsHistoryHelper(t, "success", response, "")
	if events, err := loadBeadsLifecycle(context.Background(), "/fixture/store", beads, CorrelatorOptions{}); err != nil {
		t.Fatal(err)
	} else if len(events) != 0 {
		t.Fatalf("events = %d, want 0", len(events))
	}
	assertBeadsHistoryProcess(t, fixture, bulkHistoryArgs("/fixture/store"), strings.Join(ids, "\n")+"\n")
}

func bulkHistoryArgs(store string) []string {
	return []string{"--db", store, "--readonly", "history", "--ids-file", "-", "--json"}
}

func assertBeadsHistoryProcess(t *testing.T, fixture beadsHistoryProcessFixture, wantArgs []string, wantStdin string) {
	t.Helper()
	invocations, err := os.ReadFile(fixture.invocations)
	if err != nil {
		t.Fatal(err)
	}
	if string(invocations) != "1\n" {
		t.Fatalf("invocations = %q, want exactly one", invocations)
	}
	args, err := os.ReadFile(fixture.args)
	if err != nil {
		t.Fatal(err)
	}
	wantArgBytes := append([]byte(strings.Join(wantArgs, "\x00")), 0)
	if !bytes.Equal(args, wantArgBytes) {
		t.Fatalf("argument boundaries = %q, want %q", args, wantArgBytes)
	}
	stdin, err := os.ReadFile(fixture.stdin)
	if err != nil {
		t.Fatal(err)
	}
	if string(stdin) != wantStdin {
		t.Fatalf("stdin = %q, want %q", stdin, wantStdin)
	}
}

func TestParseBulkBeadsHistoryRejectsInvalidResponses(t *testing.T) {
	validSnapshot := `{"CommitHash":"commit-1","Committer":"bot","CommitDate":"2026-01-01T00:00:00Z","Issue":{"id":"alpha","status":"open"}}`
	tests := []struct {
		name, response, want string
	}{
		{name: "malformed JSON", response: `{`, want: "invalid character"},
		{name: "trailing JSON", response: `{"schema_version":1,"issues":[]} {}`, want: "trailing JSON"},
		{name: "unknown envelope field", response: `{"schema_version":1,"issues":[],"alternate":[]}`, want: "unknown field"},
		{name: "missing schema", response: `{"issues":[]}`, want: "unsupported schema_version 0"},
		{name: "unsupported schema", response: `{"schema_version":2,"issues":[]}`, want: "unsupported schema_version 2"},
		{name: "null groups", response: `{"schema_version":1,"issues":null}`, want: "issues must be a JSON array"},
		{name: "too many groups", response: `{"schema_version":1,"issues":[{"issue_id":"alpha","snapshots":[]},{"issue_id":"beta","snapshots":[]},{"issue_id":"gamma","snapshots":[]}]}`, want: "3 issue groups for 2 requested IDs"},
		{name: "duplicate group", response: `{"schema_version":1,"issues":[{"issue_id":"alpha","snapshots":[]},{"issue_id":"alpha","snapshots":[]}]}`, want: "duplicate issue group"},
		{name: "unexpected group", response: `{"schema_version":1,"issues":[{"issue_id":"other","snapshots":[]}]}`, want: "unexpected issue group"},
		{name: "missing group", response: `{"schema_version":1,"issues":[]}`, want: "missing requested issue group"},
		{name: "wrong group order", response: `{"schema_version":1,"issues":[{"issue_id":"beta","snapshots":[]},{"issue_id":"alpha","snapshots":[]}]}`, want: "issue group 0"},
		{name: "malformed group ID", response: `{"schema_version":1,"issues":[{"issue_id":" alpha ","snapshots":[]}]}`, want: "malformed bulk History issue ID"},
		{name: "oversized group ID", response: fmt.Sprintf(`{"schema_version":1,"issues":[{"issue_id":%q,"snapshots":[]}]}`, strings.Repeat("x", 256)), want: "256 characters"},
		{name: "unknown group field", response: `{"schema_version":1,"issues":[{"issue_id":"alpha","snapshots":[],"entries":[]}]}`, want: "unknown field"},
		{name: "null snapshots", response: `{"schema_version":1,"issues":[{"issue_id":"alpha","snapshots":null}]}`, want: "snapshots must be a JSON array"},
		{name: "unknown snapshot field", response: `{"schema_version":1,"issues":[{"issue_id":"alpha","snapshots":[{"CommitHash":"x","Committer":"bot","CommitDate":"2026-01-01T00:00:00Z","Issue":{"id":"alpha","status":"open"},"Extra":true}]}]}`, want: "unknown field"},
		{name: "missing commit hash", response: `{"schema_version":1,"issues":[{"issue_id":"alpha","snapshots":[{"Committer":"bot","CommitDate":"2026-01-01T00:00:00Z","Issue":{"id":"alpha","status":"open"}}]}]}`, want: "missing CommitHash"},
		{name: "invalid timestamp", response: `{"schema_version":1,"issues":[{"issue_id":"alpha","snapshots":[{"CommitHash":"x","Committer":"bot","CommitDate":"yesterday","Issue":{"id":"alpha","status":"open"}}]}]}`, want: "lifecycle timestamp"},
		{name: "null issue", response: `{"schema_version":1,"issues":[{"issue_id":"alpha","snapshots":[{"CommitHash":"x","Committer":"bot","CommitDate":"2026-01-01T00:00:00Z","Issue":null}]}]}`, want: "Issue must be a JSON object"},
		{name: "issue ID mismatch", response: `{"schema_version":1,"issues":[{"issue_id":"alpha","snapshots":[{"CommitHash":"x","Committer":"bot","CommitDate":"2026-01-01T00:00:00Z","Issue":{"id":"beta","status":"open"}}]}]}`, want: "does not match group ID"},
		{name: "invalid status", response: `{"schema_version":1,"issues":[{"issue_id":"alpha","snapshots":[{"CommitHash":"x","Committer":"bot","CommitDate":"2026-01-01T00:00:00Z","Issue":{"id":"alpha","status":1}}]}]}`, want: "invalid status"},
		{name: "valid snapshot control with missing second group", response: `{"schema_version":1,"issues":[{"issue_id":"alpha","snapshots":[` + validSnapshot + `]}]}`, want: "missing requested issue group"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseBulkBeadsHistory([]byte(test.response), []string{"alpha", "beta"}, "/fixture/store")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestLoadBeadsLifecycleRequiresBulkCapabilityWithoutFallback(t *testing.T) {
	for _, diagnostic := range []string{
		"unknown flag: --ids-file",
		"unknown command \"history\" for \"bd\"",
		"the active storage backend does not support bulk history",
	} {
		t.Run(diagnostic, func(t *testing.T) {
			fixture := installBeadsHistoryHelper(t, "failure", "", diagnostic)
			_, err := loadBeadsLifecycle(context.Background(), "/fixture/store", []BeadInfo{{ID: "alpha"}, {ID: "beta"}}, CorrelatorOptions{})
			if err == nil || !strings.Contains(err.Error(), "bulk History support is required") || !strings.Contains(err.Error(), diagnostic) {
				t.Fatalf("error = %v", err)
			}
			assertBeadsHistoryProcess(t, fixture, bulkHistoryArgs("/fixture/store"), "alpha\nbeta\n")
		})
	}
}

func TestLoadBeadsLifecycleReportsProviderFailureWithoutCapabilityGuidance(t *testing.T) {
	fixture := installBeadsHistoryHelper(t, "failure", "", "failed to get bulk history: store query failed")
	_, err := loadBeadsLifecycle(context.Background(), "/fixture/store", []BeadInfo{{ID: "alpha"}}, CorrelatorOptions{})
	if err == nil || strings.Contains(err.Error(), "bulk History support is required") || !strings.Contains(err.Error(), "store query failed") {
		t.Fatalf("error = %v", err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error does not wrap process failure: %v", err)
	}
	assertBeadsHistoryProcess(t, fixture, bulkHistoryArgs("/fixture/store"), "alpha\n")
}

func TestLoadBeadsLifecycleReportsMissingExecutableWithoutCapabilityGuidance(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := loadBeadsLifecycle(context.Background(), "/fixture/store", []BeadInfo{{ID: "alpha"}}, CorrelatorOptions{})
	if err == nil || strings.Contains(err.Error(), "bulk History support is required") || !strings.Contains(err.Error(), "Beads executable unavailable") {
		t.Fatalf("error = %v", err)
	}
	var executableErr *exec.Error
	if !errors.As(err, &executableErr) {
		t.Fatalf("error does not wrap executable lookup failure: %v", err)
	}
}

func TestLoadBeadsLifecycleRejectsOversizedResponseAndReapsProcess(t *testing.T) {
	fixture := installBeadsHistoryHelper(t, "oversized", "", "unknown flag: --ids-file")
	_, err := loadBeadsLifecycle(context.Background(), "/fixture/store", []BeadInfo{{ID: "alpha"}}, CorrelatorOptions{})
	if err == nil || !strings.Contains(err.Error(), "bulk History response too large") || !strings.Contains(err.Error(), strconv.Itoa(beadsBulkHistoryMaxResponseBytes)) || strings.Contains(err.Error(), "bulk History support is required") {
		t.Fatalf("error = %v", err)
	}
	assertBeadsHistoryProcess(t, fixture, bulkHistoryArgs("/fixture/store"), "alpha\n")
	pidData, readErr := os.ReadFile(fixture.pid)
	if readErr != nil {
		t.Fatal(readErr)
	}
	pid, parseErr := strconv.Atoi(string(pidData))
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	process, findErr := os.FindProcess(pid)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if signalErr := process.Signal(syscall.Signal(0)); !errors.Is(signalErr, os.ErrProcessDone) {
		t.Fatalf("oversized response process %d was not reaped: %v", pid, signalErr)
	}
}

func TestBoundedDiagnosticTruncatesWithoutShortWrite(t *testing.T) {
	diagnostic := boundedDiagnostic{limit: 4}
	if written, err := diagnostic.Write([]byte("abcdef")); err != nil || written != 6 {
		t.Fatalf("Write() = %d, %v, want 6, nil", written, err)
	}
	if got := diagnostic.String(); got != "abcd [stderr truncated]" {
		t.Fatalf("String() = %q", got)
	}
}

func TestLoadBeadsLifecycleCancelsBulkProcess(t *testing.T) {
	fixture := installBeadsHistoryHelper(t, "block", "", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		_, err := loadBeadsLifecycle(ctx, "/fixture/store", []BeadInfo{{ID: "alpha"}}, CorrelatorOptions{})
		errCh <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(fixture.invocations); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("bulk History process did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bulk History process did not stop after cancellation")
	}
	invocations, readErr := os.ReadFile(fixture.invocations)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Count(string(invocations), "\n") != 1 {
		t.Fatalf("invocations = %q, want exactly one", invocations)
	}
}

func TestLoadBeadsLifecycleRejectsInvalidRequestWithoutProcess(t *testing.T) {
	tests := []struct {
		name  string
		beads []BeadInfo
		want  string
	}{
		{name: "malformed", beads: []BeadInfo{{ID: " alpha "}}, want: "malformed bulk History issue ID"},
		{name: "duplicate", beads: []BeadInfo{{ID: "alpha"}, {ID: "alpha"}}, want: "duplicate bulk History issue ID"},
		{name: "too many", beads: func() []BeadInfo {
			beads := make([]BeadInfo, beadsBulkHistoryMaxIDs+1)
			for i := range beads {
				beads[i].ID = fmt.Sprintf("work-%04d", i)
			}
			return beads
		}(), want: "at most 1000 issue IDs"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := installBeadsHistoryHelper(t, "success", `{"schema_version":1,"issues":[]}`, "")
			_, err := loadBeadsLifecycle(context.Background(), "/fixture/store", test.beads, CorrelatorOptions{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
			if _, statErr := os.Stat(fixture.invocations); !os.IsNotExist(statErr) {
				t.Fatalf("invalid request started a process: %v", statErr)
			}
		})
	}
}
