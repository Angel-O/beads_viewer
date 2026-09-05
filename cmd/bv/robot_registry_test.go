package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func TestRobotHistoryTimeoutFromMillisecondsChecked(t *testing.T) {
	tests := []struct {
		name string
		ms   int64
		want time.Duration
		ok   bool
	}{
		{name: "negative is unset", ms: -1, want: 0, ok: false},
		{name: "zero remains unbounded sentinel", ms: 0, want: 0, ok: true},
		{name: "ordinary duration", ms: 1250, want: 1250 * time.Millisecond, ok: true},
		{
			name: "largest exact millisecond duration",
			ms:   maxRobotHistoryTimeoutMillis,
			want: time.Duration(maxRobotHistoryTimeoutMillis) * time.Millisecond,
			ok:   true,
		},
		{
			name: "one millisecond beyond duration range saturates",
			ms:   maxRobotHistoryTimeoutMillis + 1,
			want: time.Duration(math.MaxInt64),
			ok:   true,
		},
		{
			name: "largest parsed integer saturates",
			ms:   math.MaxInt64,
			want: time.Duration(math.MaxInt64),
			ok:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := robotHistoryTimeoutFromMilliseconds(tt.ms)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("robotHistoryTimeoutFromMilliseconds(%d) = (%s, %v), want (%s, %v)", tt.ms, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestNoActiveHubTargetRobotsReturnEmptyPayloads(t *testing.T) {
	tests := []struct {
		name string
		run  func(RobotContext) error
	}{
		{name: "related", run: func(ctx RobotContext) error {
			id := "missing"
			return handleRobotRelated(ctx, phaseThreeRobotHandlerConfig{RobotRelatedFlag: &id})
		}},
		{name: "blocker chain", run: func(ctx RobotContext) error {
			id := "missing"
			return handleRobotBlockerChain(ctx, phaseThreeRobotHandlerConfig{RobotBlockerChainFlag: &id})
		}},
		{name: "impact network", run: func(ctx RobotContext) error {
			id := "missing"
			return handleRobotImpactNetwork(ctx, phaseThreeRobotHandlerConfig{RobotImpactNetworkFlag: &id})
		}},
		{name: "causality", run: func(ctx RobotContext) error {
			id := "missing"
			return handleRobotCausality(ctx, phaseThreeRobotHandlerConfig{RobotCausalityFlag: &id})
		}},
		{name: "sprint show", run: func(ctx RobotContext) error {
			id := "sprint-1"
			return handleRobotSprintShow(ctx, phaseThreeRobotHandlerConfig{RobotSprintShowFlag: &id})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			ctx := RobotContext{
				HubMode:     true,
				DataHash:    "empty",
				Encoder:     newJSONRobotEncoder(&output),
				ActiveScope: nil,
			}
			if err := test.run(ctx); err != nil {
				t.Fatalf("empty Hub output: %v", err)
			}
			var payload map[string]any
			if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if len(payload) == 0 {
				t.Fatal("empty JSON payload")
			}
		})
	}
}

func TestResolveRobotHistoryTimeoutSaturatesOverflow(t *testing.T) {
	t.Setenv("BV_ROBOT_HISTORY_TIMEOUT_MS", strconv.FormatInt(maxRobotHistoryTimeoutMillis+1, 10))
	unset := -1
	if got := resolveRobotHistoryTimeout(phaseThreeRobotHandlerConfig{HistoryTimeoutMs: &unset}); got != time.Duration(math.MaxInt64) {
		t.Fatalf("overflowing environment timeout = %s, want saturation at %s", got, time.Duration(math.MaxInt64))
	}
	if strconv.IntSize == 64 {
		overflowingMillis := int64(maxRobotHistoryTimeoutMillis + 1)
		overflowingFlag := int(overflowingMillis)
		if got := resolveRobotHistoryTimeout(phaseThreeRobotHandlerConfig{HistoryTimeoutMs: &overflowingFlag}); got != time.Duration(math.MaxInt64) {
			t.Fatalf("overflowing flag timeout = %s, want saturation at %s", got, time.Duration(math.MaxInt64))
		}
	}

	flagValue := 25
	if got := resolveRobotHistoryTimeout(phaseThreeRobotHandlerConfig{HistoryTimeoutMs: &flagValue}); got != 25*time.Millisecond {
		t.Fatalf("explicit flag timeout = %s, want 25ms and precedence over environment", got)
	}
}

func TestRobotRegistryValidate_RejectsModifierAlone(t *testing.T) {
	var robotTriage bool
	robotByLabel := "backend"

	registry := newRobotRegistry()
	registry.Register(RobotCommand{
		Name:        "robot-triage",
		FlagName:    "robot-triage",
		FlagPtr:     &robotTriage,
		Description: "Unified triage output",
	})
	registry.Register(RobotCommand{
		Name:            "robot-by-label",
		FlagName:        "robot-by-label",
		FlagPtr:         &robotByLabel,
		RequiredCoFlags: []string{"robot-triage", "robot-insights", "robot-plan", "robot-priority"},
		IsModifier:      true,
		Description:     "Filter robot output by label",
	})
	registry.Register(RobotCommand{
		Name:        "robot-insights",
		FlagName:    "robot-insights",
		FlagPtr:     ptrTo(false),
		Description: "Insights output",
	})
	registry.Register(RobotCommand{
		Name:        "robot-plan",
		FlagName:    "robot-plan",
		FlagPtr:     ptrTo(false),
		Description: "Plan output",
	})
	registry.Register(RobotCommand{
		Name:        "robot-priority",
		FlagName:    "robot-priority",
		FlagPtr:     ptrTo(false),
		Description: "Priority output",
	})

	err := registry.Validate()
	if err == nil {
		t.Fatal("expected modifier-alone validation error")
	}
	if !strings.Contains(err.Error(), "--robot-by-label") {
		t.Fatalf("expected error to mention modifier flag, got %q", err)
	}
	if !strings.Contains(err.Error(), "--robot-triage") {
		t.Fatalf("expected error to mention required co-flag, got %q", err)
	}

	robotTriage = true
	if err := registry.Validate(); err != nil {
		t.Fatalf("expected modifier to validate once paired with primary flag: %v", err)
	}
}

func TestTriageHistoryAvailableSkipsGitOutsideRepository(t *testing.T) {
	if triageHistoryAvailable(correlation.NewGitProvider(t.TempDir(), "issues.jsonl"), t.TempDir()) {
		t.Fatal("expected Git history to be skipped outside a repository")
	}
	if triageHistoryAvailable(correlation.NewDisabledProvider(), t.TempDir()) {
		t.Fatal("expected disabled history to be skipped")
	}
}

func TestRobotHistoryAppliesFeedbackAndExplainReportsIt(t *testing.T) {
	repo := t.TempDir()
	beadsDir := filepath.Join(repo, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runRobotTestGit(t, repo, "init", "-b", "main")
	runRobotTestGit(t, repo, "config", "user.email", "dev@example.com")
	runRobotTestGit(t, repo, "config", "user.name", "Dev")
	issuesPath := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(issuesPath, []byte(`{"id":"bv-1","title":"One","status":"open","issue_type":"task"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRobotTestGit(t, repo, "add", issuesPath)
	runRobotTestGit(t, repo, "commit", "-m", "seed tracker")

	writeCode := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte("package test\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commitSHA := func() string {
		t.Helper()
		command := exec.Command("git", "rev-parse", "HEAD")
		command.Dir = repo
		output, err := command.Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(output))
	}
	writeCode("first.go")
	runRobotTestGit(t, repo, "add", "first.go")
	runRobotTestGit(t, repo, "commit", "-m", "fix(bv-1): first")
	rejectedSHA := commitSHA()
	writeCode("second.go")
	runRobotTestGit(t, repo, "add", "second.go")
	runRobotTestGit(t, repo, "commit", "-m", "fix(bv-1): second")
	confirmedSHA := commitSHA()

	store := correlation.NewFeedbackStore(beadsDir)
	if err := store.Reject(rejectedSHA, "bv-1", "tester", 0.8, "wrong link"); err != nil {
		t.Fatal(err)
	}
	if err := store.Confirm(confirmedSHA, "bv-1", "tester", 0.8, "verified"); err != nil {
		t.Fatal(err)
	}

	issues := []model.Issue{{ID: "bv-1", Title: "One", Status: model.StatusOpen}}
	limit := 100
	var historyJSON bytes.Buffer
	if err := handleRobotHistory(RobotContext{
		Issues:          issues,
		HistoryProvider: correlation.NewGitProvider(repo, issuesPath),
		WorkDir:         repo,
		Encoder:         newJSONRobotEncoder(&historyJSON),
	}, phaseThreeRobotHandlerConfig{HistoryLimit: &limit}); err != nil {
		t.Fatalf("robot history: %v", err)
	}
	var historyOutput struct {
		Stats     correlation.HistoryStats           `json:"stats"`
		Histories map[string]correlation.BeadHistory `json:"histories"`
	}
	if err := json.Unmarshal(historyJSON.Bytes(), &historyOutput); err != nil {
		t.Fatalf("decode robot history: %v\n%s", err, historyJSON.String())
	}
	history := historyOutput.Histories["bv-1"]
	if len(history.Commits) != 1 || history.Commits[0].SHA != confirmedSHA {
		t.Fatalf("feedback-applied history commits = %+v, want only confirmed commit", history.Commits)
	}
	if history.Commits[0].Confidence != 1 || !history.Commits[0].Confirmed {
		t.Fatalf("confirmed history commit = %+v, want pinned confidence", history.Commits[0])
	}
	if applied := historyOutput.Stats.FeedbackApplied; applied == nil || applied.Confirmed != 1 || applied.Rejected != 1 {
		t.Fatalf("feedback_applied = %+v, want one confirm and reject", applied)
	}

	var explainJSON bytes.Buffer
	flag := rejectedSHA + ":bv-1"
	if err := handleRobotExplainCorrelation(RobotContext{
		Issues:          issues,
		HistoryProvider: correlation.NewGitProvider(repo, issuesPath),
		WorkDir:         repo,
		Encoder:         newJSONRobotEncoder(&explainJSON),
	}, phaseThreeRobotHandlerConfig{RobotExplainCorrFlag: &flag}); err != nil {
		t.Fatalf("robot explain: %v", err)
	}
	var explanation correlation.CorrelationExplanation
	if err := json.Unmarshal(explainJSON.Bytes(), &explanation); err != nil {
		t.Fatalf("decode robot explain: %v\n%s", err, explainJSON.String())
	}
	if explanation.Feedback == nil || explanation.Feedback.Type != correlation.FeedbackReject {
		t.Fatalf("explanation feedback = %+v, want rejection", explanation.Feedback)
	}
	if got, want := explanation.Recommendation, describeCorrelationFeedback(*explanation.Feedback); got != want {
		t.Fatalf("explanation recommendation = %q, want %q", got, want)
	}
}

func runRobotTestGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = repo
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func TestRobotRegistryAnyActive_MatchesOldLogic(t *testing.T) {
	var (
		robotHelp       bool
		robotInsights   bool
		robotTriage     bool
		robotSearch     bool
		robotFileBeads  string
		robotByLabel    string
		robotByAssignee string
		robotDocs       string
	)

	registry := newRobotRegistry()
	registry.Register(RobotCommand{Name: "robot-help", FlagName: "robot-help", FlagPtr: &robotHelp, Description: "Help"})
	registry.Register(RobotCommand{Name: "robot-insights", FlagName: "robot-insights", FlagPtr: &robotInsights, Description: "Insights"})
	registry.Register(RobotCommand{Name: "robot-triage", FlagName: "robot-triage", FlagPtr: &robotTriage, Description: "Triage"})
	registry.Register(RobotCommand{Name: "robot-search", FlagName: "robot-search", FlagPtr: &robotSearch, Description: "Search"})
	registry.Register(RobotCommand{Name: "robot-file-beads", FlagName: "robot-file-beads", FlagPtr: &robotFileBeads, Description: "File beads"})
	registry.Register(RobotCommand{
		Name:            "robot-by-label",
		FlagName:        "robot-by-label",
		FlagPtr:         &robotByLabel,
		RequiredCoFlags: []string{"robot-insights", "robot-triage"},
		IsModifier:      true,
		Description:     "Label filter",
	})
	registry.Register(RobotCommand{
		Name:            "robot-by-assignee",
		FlagName:        "robot-by-assignee",
		FlagPtr:         &robotByAssignee,
		RequiredCoFlags: []string{"robot-insights", "robot-triage"},
		IsModifier:      true,
		Description:     "Assignee filter",
	})
	registry.Register(RobotCommand{Name: "robot-docs", FlagName: "robot-docs", FlagPtr: &robotDocs, Description: "Docs"})

	oldLogic := func() bool {
		return robotHelp ||
			robotInsights ||
			robotTriage ||
			robotSearch ||
			robotFileBeads != "" ||
			robotByLabel != "" ||
			robotByAssignee != "" ||
			robotDocs != ""
	}

	tests := []struct {
		name  string
		setup func()
	}{
		{name: "none active", setup: func() {}},
		{name: "help active", setup: func() { robotHelp = true }},
		{name: "primary robot command active", setup: func() { robotTriage = true }},
		{name: "string command active", setup: func() { robotFileBeads = "pkg/ui/model.go" }},
		{name: "modifier alone still enables robot mode", setup: func() { robotByLabel = "backend" }},
		{name: "docs topic active", setup: func() { robotDocs = "commands" }},
		{name: "multiple mixed flags", setup: func() {
			robotSearch = true
			robotByAssignee = "alice"
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			robotHelp = false
			robotInsights = false
			robotTriage = false
			robotSearch = false
			robotFileBeads = ""
			robotByLabel = ""
			robotByAssignee = ""
			robotDocs = ""

			tt.setup()

			if got, want := registry.AnyActive(), oldLogic(); got != want {
				t.Fatalf("AnyActive()=%v, want %v", got, want)
			}
		})
	}
}

func TestRobotRegistryDispatchFlag_RunsActiveHandler(t *testing.T) {
	var robotHelp bool
	var called int

	registry := newRobotRegistry()
	registry.Register(RobotCommand{
		Name:     "robot-help",
		FlagName: "robot-help",
		FlagPtr:  &robotHelp,
		Handler: func(ctx RobotContext) error {
			called++
			if got := ctx.StdoutOrDefault(); got != ctx.Stdout {
				t.Fatalf("expected dispatch to preserve stdout writer")
			}
			return nil
		},
	})

	stdout := &bytes.Buffer{}
	ctx := RobotContext{Stdout: stdout}

	handled, err := registry.DispatchFlag("robot-help", ctx)
	if err != nil {
		t.Fatalf("inactive flag should not error: %v", err)
	}
	if handled {
		t.Fatal("inactive flag should not dispatch")
	}

	robotHelp = true
	handled, err = registry.DispatchFlag("robot-help", ctx)
	if err != nil {
		t.Fatalf("dispatch returned error: %v", err)
	}
	if !handled {
		t.Fatal("active flag should dispatch")
	}
	if called != 1 {
		t.Fatalf("handler call count = %d, want 1", called)
	}
}

func TestRobotRegistryDispatchUsesGenericTypedResultDecorator(t *testing.T) {
	var active bool = true
	var decorated RobotResult
	var encoded bytes.Buffer
	registry := newRobotRegistry()
	registry.Register(RobotCommand{
		Name: "robot-test", FlagName: "robot-test", FlagPtr: &active,
		Handler: func(ctx RobotContext) error {
			result := &robotPlanOutput{DataHash: "before"}
			if err := ctx.EncodeResult("robot-test", result); err != nil {
				return err
			}
			return nil
		},
	})
	ctx := RobotContext{
		Encoder: newJSONRobotEncoder(&encoded),
		ResultDecorator: func(_ string, result RobotResult) error {
			decorated = result
			result.(*robotPlanOutput).DataHash = "after"
			return nil
		},
	}
	handled, err := registry.DispatchFlag("robot-test", ctx)
	if err != nil || !handled {
		t.Fatalf("dispatch = handled:%v err:%v", handled, err)
	}
	if decorated == nil {
		t.Fatal("typed result decorator was not called")
	}
	if !strings.Contains(encoded.String(), `"data_hash":"after"`) {
		t.Fatalf("decorated result was not encoded: %s", encoded.String())
	}
}

func TestRobotLabelFlowLocalUsesNeutralLabelAdmission(t *testing.T) {
	issues := []model.Issue{
		{ID: "alpha", Labels: []string{"ctx:alpha", "api"}, Dependencies: []*model.Dependency{{DependsOnID: "beta", Type: model.DepBlocks}}},
		{ID: "beta", Labels: []string{"ctx:beta", "web"}},
	}
	var encoded bytes.Buffer
	err := handleRobotLabelFlow(RobotContext{
		Issues:   issues,
		DataHash: "hash",
		Encoder:  newJSONRobotEncoder(&encoded),
	})
	if err != nil {
		t.Fatal(err)
	}
	var output robotLabelFlowOutput
	if err := json.Unmarshal(encoded.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	wantLabels := []string{"api", "ctx:alpha", "ctx:beta", "web"}
	if !reflect.DeepEqual(output.Flow.Labels, wantLabels) {
		t.Fatalf("local label flow labels = %#v, want all direct labels", output.Flow.Labels)
	}
}

func TestRobotLabelAttentionUsesPredicateAndPinnedTimestamp(t *testing.T) {
	pinned := time.Date(2026, 8, 26, 12, 34, 56, 0, time.UTC)
	t.Setenv("SOURCE_DATE_EPOCH", strconv.FormatInt(pinned.Unix(), 10))

	var encoded bytes.Buffer
	err := handleRobotLabelAttention(RobotContext{
		Issues: []model.Issue{{
			ID:     "alpha",
			Status: model.StatusOpen,
			Labels: []string{"ctx:alpha", "api"},
		}},
		DataHash: "hash",
		Encoder:  newJSONRobotEncoder(&encoded),
		LabelPredicate: func(label string) bool {
			return !strings.HasPrefix(label, "ctx:")
		},
	}, phaseThreeRobotHandlerConfig{})
	if err != nil {
		t.Fatal(err)
	}

	var output robotAttentionOutput
	if err := json.Unmarshal(encoded.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.GeneratedAt != pinned.Format(time.RFC3339) {
		t.Fatalf("generated_at = %q, want %q", output.GeneratedAt, pinned.Format(time.RFC3339))
	}
	for _, label := range output.Labels {
		if strings.HasPrefix(label.Label, "ctx:") {
			t.Fatalf("context label was not filtered from attention output: %q", label.Label)
		}
	}
}

func TestRobotTriagePinnedHistoryStatusIsSkipped(t *testing.T) {
	pinned := time.Date(2026, 8, 26, 12, 34, 56, 0, time.UTC)
	t.Setenv("SOURCE_DATE_EPOCH", strconv.FormatInt(pinned.Unix(), 10))

	var encoded bytes.Buffer
	err := handleRobotTriage(RobotContext{
		Issues:          []model.Issue{{ID: "alpha", Status: model.StatusOpen}},
		DataHash:        "hash",
		HistoryProvider: correlation.NewDisabledProvider(),
		Encoder:         newJSONRobotEncoder(&encoded),
	}, phaseThreeRobotHandlerConfig{})
	if err != nil {
		t.Fatal(err)
	}

	var output robotTriageOutput
	if err := json.Unmarshal(encoded.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Triage.Meta.HistoryStatus != "skipped" {
		t.Fatalf("pinned history_status = %q, want skipped", output.Triage.Meta.HistoryStatus)
	}
}

func TestRobotFileHotspotsDispatchUsesSharedReportContract(t *testing.T) {
	active := true
	limit := 1
	registry := newRobotRegistry()
	registerPhaseThreeRobotHandlers(&registry, phaseThreeRobotHandlerConfig{
		RobotFileHotspotsFlag: &active,
		HotspotsLimit:         &limit,
	})

	var encoded bytes.Buffer
	handled, err := registry.DispatchFlag("robot-file-hotspots", RobotContext{
		Issues:          []model.Issue{{ID: "alpha", Status: model.StatusOpen}},
		HistoryProvider: correlation.NewDisabledProvider(),
		WorkDir:         t.TempDir(),
		Encoder:         newJSONRobotEncoder(&encoded),
	})
	if err != nil {
		t.Fatalf("dispatch robot-file-hotspots: %v", err)
	}
	if !handled {
		t.Fatal("robot-file-hotspots handler was not dispatched")
	}

	var output map[string]json.RawMessage
	if err := json.Unmarshal(encoded.Bytes(), &output); err != nil {
		t.Fatalf("decode robot-file-hotspots output: %v\n%s", err, encoded.String())
	}
	for _, field := range []string{"generated_at", "data_hash", "hotspots", "stats"} {
		if output[field] == nil {
			t.Fatalf("robot-file-hotspots output missing %q: %s", field, encoded.String())
		}
	}
}

func TestRobotTriageResultCopiesTopPicksOnce(t *testing.T) {
	input := analysis.TriageResult{QuickRef: analysis.QuickRef{TopPicks: []analysis.TopPick{
		{ID: "one"}, {ID: "two"}, {ID: "three"},
	}}}
	converted := robotTriageResultFromAnalysis(input)
	if len(converted.QuickRef.TopPicks) != len(input.QuickRef.TopPicks) {
		t.Fatalf("top picks = %d, want %d", len(converted.QuickRef.TopPicks), len(input.QuickRef.TopPicks))
	}
	for i, pick := range converted.QuickRef.TopPicks {
		if pick.ID != input.QuickRef.TopPicks[i].ID {
			t.Fatalf("top pick %d = %q, want %q", i, pick.ID, input.QuickRef.TopPicks[i].ID)
		}
	}
}

func TestRobotDiffHandlerPinsNestedTimestampWithoutMutatingInput(t *testing.T) {
	pinned := time.Date(2026, 8, 26, 12, 34, 56, 0, time.UTC)
	t.Setenv("SOURCE_DATE_EPOCH", strconv.FormatInt(pinned.Unix(), 10))

	active := true
	registry := newRobotRegistry()
	registerPhaseTwoRobotHandlers(&registry, phaseTwoRobotHandlerConfig{RobotDiffFlag: &active})

	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	originalTo := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	diff := &analysis.SnapshotDiff{FromTimestamp: from, ToTimestamp: originalTo}
	var output bytes.Buffer
	handled, err := registry.DispatchFlag("robot-diff", RobotContext{
		DataHash:             "current-hash",
		Diff:                 diff,
		DiffResolvedRevision: "abc123",
		DiffHistoricalIssues: nil,
		Encoder:              json.NewEncoder(&output),
	})
	if err != nil {
		t.Fatalf("dispatch robot-diff: %v", err)
	}
	if !handled {
		t.Fatal("robot-diff handler was not dispatched")
	}
	var decoded struct {
		GeneratedAt string `json:"generated_at"`
		Diff        struct {
			FromTimestamp time.Time `json:"from_timestamp"`
			ToTimestamp   time.Time `json:"to_timestamp"`
		} `json:"diff"`
	}
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("decode robot-diff output: %v\n%s", err, output.String())
	}
	if decoded.GeneratedAt != pinned.Format(time.RFC3339) || !decoded.Diff.ToTimestamp.Equal(pinned) {
		t.Fatalf("pinned output times = generated %q, nested %v; want %v", decoded.GeneratedAt, decoded.Diff.ToTimestamp, pinned)
	}
	if !decoded.Diff.FromTimestamp.Equal(from) {
		t.Fatalf("from timestamp = %v, want %v", decoded.Diff.FromTimestamp, from)
	}
	if !diff.ToTimestamp.Equal(originalTo) {
		t.Fatalf("handler mutated input diff timestamp to %v", diff.ToTimestamp)
	}
}

func TestDispatchRobotFlagResult_ReturnsComposableOutcome(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var robotHelp bool

		registry := newRobotRegistry()
		registry.Register(RobotCommand{
			Name:     "robot-help",
			FlagName: "robot-help",
			FlagPtr:  &robotHelp,
			Handler: func(RobotContext) error {
				return nil
			},
		})

		result := dispatchRobotFlagResult(&registry, "robot-help", RobotContext{})
		if result.Handled {
			t.Fatal("inactive flag should not dispatch")
		}
		if result.ExitCode != 0 {
			t.Fatalf("inactive flag exit code = %d, want 0", result.ExitCode)
		}

		robotHelp = true
		result = dispatchRobotFlagResult(&registry, "robot-help", RobotContext{})
		if !result.Handled {
			t.Fatal("active flag should dispatch")
		}
		if result.ExitCode != 0 {
			t.Fatalf("successful dispatch exit code = %d, want 0", result.ExitCode)
		}
		if result.Err != nil {
			t.Fatalf("successful dispatch should not return error: %v", result.Err)
		}
		if result.AlreadyReported {
			t.Fatal("successful dispatch should not be marked reported")
		}
	})

	t.Run("handler error", func(t *testing.T) {
		var robotHelp = true
		registry := newRobotRegistry()
		registry.Register(RobotCommand{
			Name:     "robot-help",
			FlagName: "robot-help",
			FlagPtr:  &robotHelp,
			Handler: func(RobotContext) error {
				return errors.New("boom")
			},
		})

		result := dispatchRobotFlagResult(&registry, "robot-help", RobotContext{})
		if !result.Handled {
			t.Fatal("active flag should dispatch")
		}
		if result.ExitCode != 1 {
			t.Fatalf("error dispatch exit code = %d, want 1", result.ExitCode)
		}
		if result.Err == nil || !strings.Contains(result.Err.Error(), "boom") {
			t.Fatalf("error dispatch returned err = %v, want boom", result.Err)
		}
		if result.AlreadyReported {
			t.Fatal("plain handler errors should not be marked reported")
		}
	})

	t.Run("reported exit", func(t *testing.T) {
		var robotHelp = true
		registry := newRobotRegistry()
		registry.Register(RobotCommand{
			Name:     "robot-help",
			FlagName: "robot-help",
			FlagPtr:  &robotHelp,
			Handler: func(RobotContext) error {
				return newReportedRobotHandlerExit(2)
			},
		})

		result := dispatchRobotFlagResult(&registry, "robot-help", RobotContext{})
		if !result.Handled {
			t.Fatal("active flag should dispatch")
		}
		if result.ExitCode != 2 {
			t.Fatalf("reported dispatch exit code = %d, want 2", result.ExitCode)
		}
		if result.Err != nil {
			t.Fatalf("reported exit should not retain wrapped error: %v", result.Err)
		}
		if !result.AlreadyReported {
			t.Fatal("reported exit should preserve AlreadyReported")
		}
	})
}

func TestWriteRobotHelp_ReturnsWriterError(t *testing.T) {
	err := writeRobotHelp(failingWriter{err: errors.New("write failed")})
	if err == nil {
		t.Fatal("expected writer error")
	}
	if !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected wrapped writer error, got %v", err)
	}
}

func TestWriteRobotHelp_ReturnsWriterErrorAfterIntro(t *testing.T) {
	writer := &failAfterNWritesWriter{
		failAfter: 1,
		err:       errors.New("write failed after intro"),
	}

	err := writeRobotHelp(writer)
	if err == nil {
		t.Fatal("expected writer error after intro")
	}
	// The first write after the intro is the generated commands heading.
	if !strings.Contains(err.Error(), "commands heading") {
		t.Fatalf("expected contextual error for later write, got %v", err)
	}
	if !strings.Contains(err.Error(), "write failed after intro") {
		t.Fatalf("expected underlying writer error, got %v", err)
	}
}

func TestFilterOrphanReportByMinScoreRebuildsDerivedFields(t *testing.T) {
	report := &correlation.OrphanReport{
		Stats: correlation.OrphanReportStats{
			CandidateCount: 2,
			AvgSuspicion:   70,
		},
		Candidates: []correlation.OrphanCandidate{
			{
				ShortSHA:       "aaaaaaa",
				SuspicionScore: 90,
				ProbableBeads:  []correlation.ProbableBead{{BeadID: "bv-keep"}},
			},
			{
				ShortSHA:       "bbbbbbb",
				SuspicionScore: 20,
				ProbableBeads:  []correlation.ProbableBead{{BeadID: "bv-drop"}},
			},
		},
		ByBead: map[string][]string{
			"bv-keep": []string{"aaaaaaa"},
			"bv-drop": []string{"bbbbbbb"},
		},
	}

	filterOrphanReportByMinScore(report, 50)

	if len(report.Candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(report.Candidates))
	}
	if strings.Compare(report.Candidates[0].ShortSHA, "aaaaaaa") != 0 {
		t.Fatalf("candidate short SHA = %q, want aaaaaaa", report.Candidates[0].ShortSHA)
	}
	if report.Stats.CandidateCount != 1 {
		t.Fatalf("stats candidate count = %d, want 1", report.Stats.CandidateCount)
	}
	if report.Stats.AvgSuspicion != 90 {
		t.Fatalf("avg suspicion = %v, want 90", report.Stats.AvgSuspicion)
	}
	if got := report.ByBead["bv-keep"]; len(got) != 1 || strings.Compare(got[0], "aaaaaaa") != 0 {
		t.Fatalf("kept by_bead entry = %#v, want aaaaaaa", got)
	}
	if dropped := report.ByBead["bv-drop"]; dropped != nil {
		t.Fatalf("dropped candidate still present in by_bead: %#v", dropped)
	}

	filterOrphanReportByMinScore(report, 101)
	if len(report.Candidates) != 0 {
		t.Fatalf("candidate count after filtering all = %d, want 0", len(report.Candidates))
	}
	if report.Stats.CandidateCount != 0 {
		t.Fatalf("stats candidate count after filtering all = %d, want 0", report.Stats.CandidateCount)
	}
	if report.Stats.AvgSuspicion != 0 {
		t.Fatalf("avg suspicion after filtering all = %v, want 0", report.Stats.AvgSuspicion)
	}
	if len(report.ByBead) != 0 {
		t.Fatalf("by_bead after filtering all = %#v, want empty", report.ByBead)
	}
}

func TestParseCorrelationArgTrimsAndRejectsEmptyParts(t *testing.T) {
	commitSHA, beadID, err := parseCorrelationArg("  abc123 : bv-1  ")
	if err != nil {
		t.Fatalf("parseCorrelationArg returned error: %v", err)
	}
	if commitSHA != "abc123" {
		t.Fatalf("commit SHA = %q, want abc123", commitSHA)
	}
	if beadID != "bv-1" {
		t.Fatalf("bead ID = %q, want bv-1", beadID)
	}

	tests := []string{
		"",
		"abc123",
		":bv-1",
		"abc123:",
		"   :   ",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, _, err := parseCorrelationArg(input); err == nil {
				t.Fatalf("parseCorrelationArg(%q) succeeded, want error", input)
			}
		})
	}
}

func TestResolveCorrelatedCommitRejectsAmbiguousPrefix(t *testing.T) {
	commits := []correlation.CorrelatedCommit{
		{SHA: "abc123def456", ShortSHA: "abc123d", Confidence: 0.8},
		{SHA: "abc123fff000", ShortSHA: "abc123f", Confidence: 0.7},
	}

	commit, err := resolveCorrelatedCommit(commits, "abc123d")
	if err != nil {
		t.Fatalf("resolveCorrelatedCommit returned error: %v", err)
	}
	if commit == nil || commit.SHA != "abc123def456" {
		t.Fatalf("resolved commit = %#v, want abc123def456", commit)
	}

	commit, err = resolveCorrelatedCommit(commits, "ABC123F")
	if err != nil {
		t.Fatalf("resolveCorrelatedCommit uppercase short SHA returned error: %v", err)
	}
	if commit == nil || commit.SHA != "abc123fff000" {
		t.Fatalf("uppercase resolved commit = %#v, want abc123fff000", commit)
	}

	commit, err = resolveCorrelatedCommit(commits, "abc123")
	if err == nil {
		t.Fatal("expected ambiguous prefix error")
	}
	if commit != nil {
		t.Fatalf("commit = %#v, want nil on ambiguity", commit)
	}
	if !strings.Contains(err.Error(), "ambiguous commit SHA prefix") {
		t.Fatalf("error = %q, want ambiguity message", err.Error())
	}
}

func ptrTo[T any](v T) *T {
	return &v
}

type failingWriter struct {
	err error
}

func (w failingWriter) Write([]byte) (int, error) {
	return 0, w.err
}

type failAfterNWritesWriter struct {
	failAfter int
	writes    int
	err       error
}

func (w *failAfterNWritesWriter) Write(p []byte) (int, error) {
	if w.writes >= w.failAfter {
		return 0, w.err
	}
	w.writes++
	return len(p), nil
}
