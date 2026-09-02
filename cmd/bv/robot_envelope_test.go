package main

import (
	"reflect"
	"testing"
)

// Every robot payload embeds the envelope built from the dispatch context, so
// the source, time-travel, and scoping metadata cannot be forgotten per handler.
func TestRobotContext_EnvelopeCarriesSourceScopeAndAsOf(t *testing.T) {
	ctx := RobotContext{
		DataHash:   "abc",
		SourcePath: "/repo/.beads/issues.jsonl",
		SourceKind: "jsonl_local",
	}
	env := ctx.Envelope()
	if env.DataHash != "abc" || env.SourcePath != "/repo/.beads/issues.jsonl" || env.SourceKind != "jsonl_local" {
		t.Fatalf("envelope = %+v", env)
	}
	if env.Scope != nil {
		t.Fatalf("no scoping flags -> scope must be omitted, got %+v", env.Scope)
	}
	if env.AsOf != "" || env.AsOfCommit != "" {
		t.Fatalf("no --as-of -> as_of fields empty, got %+v", env)
	}

	ctx.LabelScope = "backend"
	ctx.Recipe = "actionable"
	ctx.Repo = "api"
	env = ctx.Envelope()
	if env.Scope == nil || env.Scope.Label != "backend" || env.Scope.Recipe != "actionable" || env.Scope.Repo != "api" {
		t.Fatalf("scope not propagated: %+v", env.Scope)
	}
	if len(env.Scope.Unsupported) != 0 {
		t.Fatalf("without --as-of nothing is unsupported, got %v", env.Scope.Unsupported)
	}

	// A derived hash replaces the context hash but keeps everything else.
	if got := ctx.EnvelopeWithHash("zzz"); got.DataHash != "zzz" || got.SourcePath != ctx.SourcePath || got.Scope == nil {
		t.Fatalf("EnvelopeWithHash = %+v", got)
	}
}

func TestRobotContext_EnvelopeDeclaresUnsupportedAsOf(t *testing.T) {
	ctx := RobotContext{DataHash: "h", AsOf: "HEAD~5", AsOfCommit: "0123456789abcdef", SourceKind: "git", SourcePath: ".beads@HEAD~5"}

	// Commands that analyse ctx.Issues honour --as-of: no unsupported list.
	for _, cmd := range []string{"robot-triage", "robot-plan", "robot-insights", "robot-blocker-chain", "robot-priority"} {
		ctx.Command = cmd
		env := ctx.Envelope()
		if env.AsOf != "HEAD~5" || env.AsOfCommit != "0123456789abcdef" {
			t.Fatalf("%s: as_of metadata missing: %+v", cmd, env)
		}
		if env.Scope != nil && len(env.Scope.Unsupported) > 0 {
			t.Fatalf("%s must honour --as-of, got unsupported %v", cmd, env.Scope.Unsupported)
		}
	}

	// Commands that read sprint files from disk or walk live git history cannot.
	for _, cmd := range []string{"robot-burndown", "robot-sprint-list", "robot-forecast", "robot-history", "robot-orphans", "robot-file-beads"} {
		ctx.Command = cmd
		env := ctx.Envelope()
		if env.Scope == nil || !reflect.DeepEqual(env.Scope.Unsupported, []string{"as_of"}) {
			t.Fatalf("%s must declare as_of unsupported, got %+v", cmd, env.Scope)
		}
	}

	// The declaration only exists while time-travelling.
	ctx.AsOf, ctx.AsOfCommit = "", ""
	ctx.Command = "robot-burndown"
	if env := ctx.Envelope(); env.Scope != nil {
		t.Fatalf("no --as-of: burndown scope must be omitted, got %+v", env.Scope)
	}
}

// DispatchFlag stamps the normalized command onto the context so the envelope
// can consult the per-command capability table.
func TestRobotRegistry_DispatchFlagSetsCommand(t *testing.T) {
	active := true
	var seen string
	reg := &RobotRegistry{}
	reg.Register(RobotCommand{
		Name:     "probe",
		FlagName: "robot-probe",
		FlagPtr:  &active,
		Handler: func(ctx RobotContext) error {
			seen = ctx.Command
			return nil
		},
	})
	handled, err := reg.DispatchFlag("--robot-probe", RobotContext{})
	if err != nil || !handled {
		t.Fatalf("dispatch: handled=%v err=%v", handled, err)
	}
	if seen != "robot-probe" {
		t.Fatalf("ctx.Command = %q, want robot-probe", seen)
	}
}
