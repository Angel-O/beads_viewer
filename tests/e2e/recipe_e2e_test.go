package main_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// recipeProject writes a small project whose issues carry a "sprint" label
// on some items, plus a project recipe file under .beads/recipes that keeps
// only open sprint work. Returns the project dir.
func recipeProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeIssuesJSONL(t, dir, strings.Join([]string{
		`{"id":"SP-1","title":"Sprint root","status":"open","priority":1,"issue_type":"task","labels":["sprint"]}`,
		`{"id":"SP-2","title":"Sprint follow-up","status":"open","priority":2,"issue_type":"task","labels":["sprint"],"dependencies":[{"issue_id":"SP-2","depends_on_id":"SP-1","type":"blocks"}]}`,
		`{"id":"BL-1","title":"Backlog item","status":"open","priority":0,"issue_type":"task","labels":["backlog"]}`,
		`{"id":"SP-9","title":"Sprint done","status":"closed","priority":1,"issue_type":"task","labels":["sprint"]}`,
	}, "\n")+"\n")
	writeRecipeFile(t, dir, "sprint.yaml", `description: "Open sprint work"
filters:
  status: [open, in_progress]
  tags: [sprint]
sort:
  field: priority
  secondary:
    field: id
`)
	return dir
}

func writeRecipeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	recipesDir := filepath.Join(dir, ".beads", "recipes")
	if err := os.MkdirAll(recipesDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", recipesDir, err)
	}
	path := filepath.Join(recipesDir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// runBVSplit runs bv and returns stdout and stderr separately along with the
// exit error, for tests that must inspect diagnostics on both outcomes.
func runBVSplit(t *testing.T, dir string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(buildBvBinary(t), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "BV_NO_BROWSER=1", "BV_TEST_MODE=1", "TERM=dumb")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

type recipePlanPayload struct {
	DataHash string `json:"data_hash"`
	Plan     struct {
		Tracks []struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"tracks"`
		TotalActionable int `json:"total_actionable"`
		TotalBlocked    int `json:"total_blocked"`
	} `json:"plan"`
}

func planIssueIDs(p recipePlanPayload) map[string]bool {
	ids := make(map[string]bool)
	for _, track := range p.Plan.Tracks {
		for _, item := range track.Items {
			ids[item.ID] = true
		}
	}
	return ids
}

// TestRecipePathArgumentRobotPlan: `--recipe .beads/recipes/x.yaml --robot-plan`
// exits 0 and the plan contains only the recipe's issues; the same file is
// also addressable by its stem.
func TestRecipePathArgumentRobotPlan(t *testing.T) {
	dir := recipeProject(t)

	for _, arg := range []string{filepath.Join(".beads", "recipes", "sprint.yaml"), "sprint"} {
		t.Run(arg, func(t *testing.T) {
			var payload recipePlanPayload
			if err := runBVCommandJSON(t, dir, &payload, "--recipe", arg, "--robot-plan"); err != nil {
				t.Fatalf("--recipe %s --robot-plan: %v", arg, err)
			}
			if payload.DataHash == "" {
				t.Fatalf("missing data_hash")
			}
			ids := planIssueIDs(payload)
			if !ids["SP-1"] {
				t.Fatalf("plan is missing the actionable sprint issue SP-1: %v", ids)
			}
			for id := range ids {
				if id != "SP-1" && id != "SP-2" {
					t.Fatalf("plan contains %s, which the recipe filters out (%v)", id, ids)
				}
			}
			// BL-1 is open and actionable but not sprint-labelled; SP-9 is closed.
			// The plan's totals must reflect the two-issue recipe scope, not all four.
			if total := payload.Plan.TotalActionable + payload.Plan.TotalBlocked; total != 2 {
				t.Fatalf("plan scope = %d issues (actionable=%d blocked=%d), want 2", total, payload.Plan.TotalActionable, payload.Plan.TotalBlocked)
			}
		})
	}

	// Without the recipe the backlog item is planned too, proving the filter did the work.
	var unfiltered recipePlanPayload
	if err := runBVCommandJSON(t, dir, &unfiltered, "--robot-plan"); err != nil {
		t.Fatalf("--robot-plan: %v", err)
	}
	if !planIssueIDs(unfiltered)["BL-1"] {
		t.Fatalf("unfiltered plan should include BL-1: %v", planIssueIDs(unfiltered))
	}
}

// TestRecipeHighImpactRobotTriage: the builtin high-impact recipe (a PageRank
// sort) still drives --robot-triage.
func TestRecipeHighImpactRobotTriage(t *testing.T) {
	dir := t.TempDir()
	writeIssuesJSONL(t, dir, strings.Join([]string{
		`{"id":"ROOT","title":"Root blocker","status":"open","priority":3,"issue_type":"task"}`,
		`{"id":"MID","title":"Middle","status":"open","priority":2,"issue_type":"task","dependencies":[{"issue_id":"MID","depends_on_id":"ROOT","type":"blocks"}]}`,
		`{"id":"LEAF","title":"Leaf","status":"open","priority":1,"issue_type":"task","dependencies":[{"issue_id":"LEAF","depends_on_id":"MID","type":"blocks"}]}`,
		`{"id":"SOLO","title":"Independent","status":"open","priority":0,"issue_type":"task"}`,
		`{"id":"DONE","title":"Finished","status":"closed","priority":0,"issue_type":"task"}`,
	}, "\n")+"\n")

	var payload struct {
		DataHash string `json:"data_hash"`
		Triage   struct {
			Recommendations []struct {
				ID string `json:"id"`
			} `json:"recommendations"`
		} `json:"triage"`
	}
	if err := runBVCommandJSON(t, dir, &payload, "--recipe", "high-impact", "--robot-triage"); err != nil {
		t.Fatalf("--recipe high-impact --robot-triage: %v", err)
	}
	if payload.DataHash == "" {
		t.Fatalf("missing data_hash")
	}
	if len(payload.Triage.Recommendations) == 0 {
		t.Fatalf("expected triage recommendations under the high-impact recipe")
	}
	for _, rec := range payload.Triage.Recommendations {
		if rec.ID == "DONE" {
			t.Fatalf("high-impact keeps only open/in_progress issues; got closed DONE in %+v", payload.Triage.Recommendations)
		}
	}
}

// TestRecipeProjectFileListedInRobotRecipes: --robot-recipes reports the
// source (and defining path) of every recipe, project files included.
func TestRecipeProjectFileListedInRobotRecipes(t *testing.T) {
	dir := recipeProject(t)
	var payload struct {
		Recipes []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Source      string `json:"source"`
			Path        string `json:"path"`
		} `json:"recipes"`
	}
	if err := runBVCommandJSON(t, dir, &payload, "--robot-recipes"); err != nil {
		t.Fatalf("--robot-recipes: %v", err)
	}
	seen := map[string]string{}
	for _, r := range payload.Recipes {
		seen[r.Name] = r.Source
		if r.Name != "sprint" {
			continue
		}
		if r.Source != "project-file" {
			t.Fatalf("sprint source = %q, want project-file", r.Source)
		}
		if r.Description != "Open sprint work" {
			t.Fatalf("sprint description = %q", r.Description)
		}
		if !strings.HasSuffix(r.Path, filepath.Join(".beads", "recipes", "sprint.yaml")) {
			t.Fatalf("sprint path = %q, want the defining file", r.Path)
		}
	}
	if seen["sprint"] == "" {
		t.Fatalf("project-file recipe 'sprint' not listed; got %v", seen)
	}
	if seen["high-impact"] != "builtin" || seen["actionable"] != "builtin" {
		t.Fatalf("builtin recipes should report source builtin: %v", seen)
	}
}

// TestRecipeUnknownFilterKeyRejected: a recipe file with a misspelt filter key
// fails with the key named, whether addressed by path or by name.
func TestRecipeUnknownFilterKeyRejected(t *testing.T) {
	dir := recipeProject(t)
	bad := writeRecipeFile(t, dir, "bad.yaml", `description: "typo in filter key"
filters:
  statuses: [open]
`)

	stdout, stderr, err := runBVSplit(t, dir, "--recipe", bad, "--robot-plan")
	if err == nil {
		t.Fatalf("--recipe %s should fail; stdout=%s", bad, stdout)
	}
	if !strings.Contains(stderr, "statuses") || !strings.Contains(stderr, "bad.yaml") {
		t.Fatalf("stderr should name the unknown key and file:\n%s", stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("no JSON must be emitted for a rejected recipe; stdout=%s", stdout)
	}

	stdout, stderr, err = runBVSplit(t, dir, "--recipe", "bad", "--robot-plan")
	if err == nil {
		t.Fatalf("--recipe bad should fail; stdout=%s", stdout)
	}
	if !strings.Contains(stderr, `unknown recipe "bad"`) || !strings.Contains(stderr, "statuses") {
		t.Fatalf("stderr should say the name is unknown and why the file was skipped:\n%s", stderr)
	}

	// The good sibling file still loads despite the bad one.
	var payload recipePlanPayload
	if err := runBVCommandJSON(t, dir, &payload, "--recipe", "sprint", "--robot-plan"); err != nil {
		t.Fatalf("--recipe sprint after a bad sibling: %v", err)
	}
}

// TestRecipeMissingPathErrors: a path that does not exist is a clear error,
// not an "unknown recipe" with a list of builtins.
func TestRecipeMissingPathErrors(t *testing.T) {
	dir := recipeProject(t)
	missing := filepath.Join(".beads", "recipes", "nope.yaml")
	stdout, stderr, err := runBVSplit(t, dir, "--recipe", missing, "--robot-plan")
	if err == nil {
		t.Fatalf("--recipe %s should fail; stdout=%s", missing, stdout)
	}
	if !strings.Contains(stderr, "recipe file not found") || !strings.Contains(stderr, "nope.yaml") {
		t.Fatalf("stderr should say the recipe file was not found:\n%s", stderr)
	}
}

// TestRecipeUnappliedFieldsWarn: view/export fields that are parsed but not
// honoured yet produce a one-line warning each instead of being ignored.
func TestRecipeUnappliedFieldsWarn(t *testing.T) {
	dir := recipeProject(t)
	writeRecipeFile(t, dir, "cols.yaml", `filters:
  status: [open]
view:
  columns: [id, title]
  max_items: 1
export:
  format: markdown
`)
	stdout, stderr, err := runBVSplit(t, dir, "--recipe", "cols", "--robot-plan")
	if err != nil {
		t.Fatalf("--recipe cols --robot-plan: %v\nstderr=%s", err, stderr)
	}
	var payload recipePlanPayload
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("plan JSON: %v\n%s", err, stdout)
	}
	for _, want := range []string{"recipe cols: view.columns is not applied yet", "recipe cols: export.format is not applied yet"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, "max_items") {
		t.Fatalf("view.max_items is applied and must not be warned about:\n%s", stderr)
	}
	// max_items: 1 narrows the plan to a single issue.
	if total := payload.Plan.TotalActionable + payload.Plan.TotalBlocked; total != 1 {
		t.Fatalf("plan scope = %d issues, want 1 (view.max_items)", total)
	}
}
