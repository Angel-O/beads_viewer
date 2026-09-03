package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
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
	for _, cmd := range []string{"robot-triage", "robot-plan", "robot-insights", "robot-blocker-chain", "robot-priority", "robot-forecast", "robot-capacity"} {
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
	for _, cmd := range []string{"robot-burndown", "robot-sprint-list", "robot-sprint-show", "robot-history", "bead-history", "robot-orphans", "robot-file-beads", "robot-file-hotspots"} {
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

func TestRobotTypedResultEnvelopeMergesCLIAndHubScope(t *testing.T) {
	var encoded bytes.Buffer
	result := &robotPlanOutput{
		GeneratedAt: "stale",
		DataHash:    "stale",
		Scope: &robotScopeMetadata{
			Mode:               "selected_contexts",
			Contexts:           []string{"ctx:alpha"},
			IncludeContextless: false,
		},
	}
	ctx := RobotContext{
		DataHash:   "current",
		SourcePath: "/repo/.beads/issues.jsonl",
		SourceKind: "jsonl",
		AsOf:       "HEAD~3",
		AsOfCommit: "0123456789abcdef",
		LabelScope: "backend",
		Recipe:     "actionable",
		Repo:       "api",
		Command:    "robot-file-hotspots",
		Encoder:    newJSONRobotEncoder(&encoded),
		ResultDecorator: func(_ string, value RobotResult) error {
			value.(*robotPlanOutput).Scope.Mode = "selected_contexts"
			return nil
		},
	}
	if err := ctx.EncodeResult("robot-plan", result); err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded.Bytes(), &payload); err != nil {
		t.Fatalf("decode typed result: %v\n%s", err, encoded.String())
	}
	if payload["source_path"] != "/repo/.beads/issues.jsonl" || payload["source_kind"] != "jsonl" {
		t.Fatalf("source metadata = %#v", payload)
	}
	if payload["as_of"] != "HEAD~3" || payload["as_of_commit"] != "0123456789abcdef" {
		t.Fatalf("as-of metadata = %#v", payload)
	}
	scope, ok := payload["scope"].(map[string]any)
	if !ok {
		t.Fatalf("scope = %#v", payload["scope"])
	}
	for key, want := range map[string]any{
		"label": "backend", "recipe": "actionable", "repo": "api", "mode": "selected_contexts",
	} {
		if scope[key] != want {
			t.Fatalf("scope[%q] = %#v, want %#v", key, scope[key], want)
		}
	}
	unsupported, ok := scope["unsupported"].([]any)
	if !ok || len(unsupported) != 1 || unsupported[0] != "as_of" {
		t.Fatalf("scope.unsupported = %#v", scope["unsupported"])
	}
	if strings.Contains(encoded.String(), `"scope":null`) {
		t.Fatalf("scope collision dropped metadata: %s", encoded.String())
	}
}

func TestRobotTypedResultEnvelopePreservesPureRepoScope(t *testing.T) {
	var encoded bytes.Buffer
	ctx := RobotContext{
		DataHash:   "current",
		SourcePath: "/repo/.beads/issues.jsonl",
		SourceKind: "jsonl",
		Repo:       "api",
		Encoder:    newJSONRobotEncoder(&encoded),
	}
	if err := ctx.EncodeResult("robot-plan", &robotPlanOutput{DataHash: ctx.DataHash}); err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	scope, ok := payload["scope"].(map[string]any)
	if !ok || scope["repo"] != "api" {
		t.Fatalf("pure-repo scope = %#v", payload["scope"])
	}
}

func TestRobotDirectOutputsReceiveContextualEnvelope(t *testing.T) {
	commands := []string{
		"robot-file-beads",
		"robot-file-relations",
		"robot-impact",
		"robot-related",
		"robot-impact-network",
		"robot-causality",
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			var encoded bytes.Buffer
			ctx := RobotContext{
				SourcePath: "/repo/.beads/issues.jsonl",
				SourceKind: "jsonl",
				AsOf:       "HEAD~3",
				AsOfCommit: "0123456789abcdef",
				Command:    command,
				Encoder:    newJSONRobotEncoder(&encoded),
			}
			payload := struct {
				DataHash string `json:"data_hash"`
				Value    string `json:"value"`
			}{DataHash: "report-hash", Value: command}
			if err := ctx.EncodePayload(ctx.EnvelopeWithHash("report-hash"), payload); err != nil {
				t.Fatal(err)
			}

			var output map[string]any
			if err := json.Unmarshal(encoded.Bytes(), &output); err != nil {
				t.Fatal(err)
			}
			if output["data_hash"] != "report-hash" || output["source_kind"] != "jsonl" || output["as_of"] != "HEAD~3" || output["as_of_commit"] != "0123456789abcdef" || output["value"] != command {
				t.Fatalf("contextual envelope output = %#v", output)
			}
			scope, ok := output["scope"].(map[string]any)
			if !ok {
				t.Fatalf("scope = %#v", output["scope"])
			}
			unsupported, ok := scope["unsupported"].([]any)
			if !ok || len(unsupported) != 1 || unsupported[0] != "as_of" {
				t.Fatalf("scope.unsupported = %#v", scope["unsupported"])
			}
		})
	}
}
