package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
)

func TestParseBulkLabels(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantOp     string
		wantLabels string
	}{
		{"explicit add", []string{"labels", "add", "issue-b", "issue-a", "--labels", " zeta, alpha ", "--labels", "alpha", "--json"}, "add", "zeta,alpha"},
		{"epic remove", []string{"--json", "labels", "remove", "--epic", "epic-a", "--labels", "old"}, "remove", "old"},
		{"replace clear", []string{"labels", "replace", "issue-a", "--clear", "--json"}, "replace", ""},
		{"replace labels", []string{"labels", "replace", "issue-a", "--labels=x,y", "--json"}, "replace", "x,y"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := parse(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if request.labelsOperation != test.wantOp || strings.Join(request.labels, ",") != test.wantLabels {
				t.Fatalf("parsed operation=%q labels=%v", request.labelsOperation, request.labels)
			}
		})
	}
}

func TestParseBulkLabelsRejectsUnsafeAndAmbiguousRequests(t *testing.T) {
	tests := [][]string{
		{"labels", "add", "issue-a", "--labels", "x"},
		{"labels", "add", "issue-a", "issue-b", "--epic", "epic-a", "--labels", "x", "--json"},
		{"labels", "add", "issue-a", "--labels", "ctx:private", "--json"},
		{"labels", "remove", "issue-a", "--labels", "", "--json"},
		{"labels", "replace", "issue-a", "--labels", "x", "--clear", "--json"},
		{"labels", "replace", "issue-a", "--json"},
		{"labels", "add", "issue-a", "--labels", "x,,y", "--json"},
		{"labels", "add", "issue-a", "--labels", `"quoted"`, "--json"},
		{"labels", "add", "issue-a", "--labels", "x\ny", "--json"},
		{"labels", "add", "issue-a", "--labels", "x\x00y", "--json"},
		{"labels", "add", "issue-a", "--labels", "x\x1by", "--json"},
		{"labels", "remove", "issue-a", "--labels", "x\x7fy", "--json"},
		{"labels", "add", "issue-a", "--labels", strings.Repeat("x", 256), "--json"},
	}
	for _, args := range tests {
		if _, err := parse(args); err == nil {
			t.Errorf("parse(%q) unexpectedly succeeded", args)
		}
	}
}

func TestBulkLabelResponseDecoders(t *testing.T) {
	ids, err := decodeBulkChildIDs([]byte(`[{"id":"issue-b"},{"id":"issue-a"},{"id":"issue-a"}]`))
	if err != nil || len(ids) != 3 || ids[0] != "issue-b" {
		t.Fatalf("decode child IDs = %v, %v", ids, err)
	}
	if _, err := decodeBulkChildIDs([]byte(`[{}]`)); err == nil {
		t.Fatal("accepted child without an ID")
	}
	post, err := decodeBulkLabelPostState([]byte(`[{"id":"issue-a","issue_type":"task","status":"open","labels":["ctx:synthetic","x"]}]`), "issue-a")
	if err != nil || post.ID != "issue-a" {
		t.Fatalf("valid post-state = %#v, %v", post, err)
	}
	if _, err := decodeBulkLabelPostState([]byte(`[{"id":"issue-b","issue_type":"task","status":"open","labels":[]}]`), "issue-a"); err == nil {
		t.Fatal("accepted a different returned ID")
	}
	post, err = decodeBulkLabelPostState([]byte(`[{"id":"todo-a","issue_type":"todo","status":"open"}]`), "todo-a")
	if err != nil || post.Labels == nil || len(post.Labels) != 0 {
		t.Fatalf("omitted empty labels = %#v, %v", post.Labels, err)
	}
	for _, malformed := range []string{
		`[{"id":"todo-a","issue_type":"todo","status":"open","labels":null}]`,
		`[{"id":"todo-a","issue_type":"todo","status":"open","labels":"not-an-array"}]`,
		`[{"id":"todo-a","issue_type":"todo","status":"open","labels":[1]}]`,
	} {
		if _, err := decodeBulkLabelPostState([]byte(malformed), "todo-a"); err == nil {
			t.Errorf("accepted malformed labels: %s", malformed)
		}
	}
	if got := sortedUnique([]string{"z", "a", "z"}); strings.Join(got, ",") != "a,z" {
		t.Fatalf("sortedUnique = %v", got)
	}
}

func TestBulkLabelsLaterPreflightFailureBlocksEveryWrite(t *testing.T) {
	test := newAppTest(t, false)
	ctx := "ctx:synthetic-project"
	writeHubConfig(t, test, map[string]string{ctx: test.repository})
	setResponses(t, map[string]string{
		"show:work-a": `[{"id":"work-a","status":"open","issue_type":"task","labels":["ctx:synthetic-project"]}]`,
		"show:work-z": `[{"id":"work-z","status":"open","issue_type":"task","labels":["ctx:unregistered"]}]`,
	})
	code, stdout, _ := test.run("labels", "remove", "work-z", "work-a", "--labels", "old", "--json")
	if code == 0 || stdout != "" {
		t.Fatalf("later target preflight code=%d stdout=%q", code, stdout)
	}
	showCount, writes := 0, 0
	for _, call := range test.calls() {
		switch {
		case strings.HasPrefix(fakeCommandKey(call.Args), "show:"):
			showCount++
		case strings.HasPrefix(fakeCommandKey(call.Args), "update:"):
			writes++
		}
	}
	if showCount != 2 || writes != 0 {
		t.Fatalf("preflight reads=%d writes=%d", showCount, writes)
	}
}

func TestBulkLabelsEmptyEpicSelectionSucceeds(t *testing.T) {
	test := newAppTest(t, false)
	writeHubConfig(t, test, map[string]string{})
	setResponses(t, map[string]string{
		"show:epic-empty":               `[ {"id":"epic-empty","status":"open","issue_type":"epic","labels":[]} ]`,
		"epic:child-closure:epic-empty": `[]`,
	})
	code, stdout, stderr := test.run("labels", "replace", "--epic", "epic-empty", "--clear", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q stdout=%s", code, stderr, stdout)
	}
	var result bulkLabelResponse
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result.Matched != 0 || result.Applied != 0 || result.Errors != 0 || result.Results == nil || len(result.Results) != 0 {
		t.Fatalf("empty epic outcome = %#v", result)
	}
	for _, call := range test.calls() {
		if strings.HasPrefix(fakeCommandKey(call.Args), "update:") {
			t.Fatalf("empty epic wrote: %#v", call)
		}
	}
}

func TestBulkLabelsOmittedEmptyPostStateSucceeds(t *testing.T) {
	for _, testCase := range []struct {
		name, kind, op string
		args           []string
	}{
		{"contextless todo remove no-op", "todo", "remove", []string{"--labels", "absent"}},
		{"empty-context epic clear", "epic", "replace", []string{"--clear"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			test := newAppTest(t, false)
			writeHubConfig(t, test, map[string]string{})
			setResponses(t, map[string]string{
				"show:contextless":   fmt.Sprintf(`[{"id":"contextless","status":"open","issue_type":%q,"labels":[]}]`, testCase.kind),
				"update:contextless": fmt.Sprintf(`[{"id":"contextless","status":"open","issue_type":%q}]`, testCase.kind),
			})
			args := []string{"labels", testCase.op, "contextless"}
			args = append(args, testCase.args...)
			args = append(args, "--json")
			code, stdout, stderr := test.run(args...)
			if code != 0 || stderr != "" {
				t.Fatalf("code=%d stderr=%q stdout=%s", code, stderr, stdout)
			}
			var result bulkLabelResponse
			if err := json.Unmarshal([]byte(stdout), &result); err != nil {
				t.Fatal(err)
			}
			if result.Applied != 1 || result.Errors != 0 || result.Results[0].Labels == nil || len(*result.Results[0].Labels) != 0 {
				t.Fatalf("omitted labels result = %#v", result)
			}
		})
	}
}

func TestBulkLabelsMalformedSuccessfulUpdateIsUnknown(t *testing.T) {
	test := newAppTest(t, false)
	ctx := "ctx:synthetic-project"
	writeHubConfig(t, test, map[string]string{ctx: test.repository})
	setResponses(t, map[string]string{
		"show:work-a":   `[{"id":"work-a","status":"open","issue_type":"task","labels":["ctx:synthetic-project"]}]`,
		"update:work-a": `{"unexpected":"object"}`,
	})
	code, stdout, stderr := test.run("labels", "add", "work-a", "--labels", "tag", "--json")
	if code != 1 || !strings.Contains(stderr, `"code":"bulk_label_partial_failure"`) {
		t.Fatalf("code=%d stderr=%q stdout=%s", code, stderr, stdout)
	}
	var result bulkLabelResponse
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result.Errors != 1 || len(result.Results) != 1 || !result.Results[0].OutcomeUnknown || result.Results[0].Labels != nil {
		t.Fatalf("malformed success outcome = %#v", result)
	}
}

type labelFailWriter struct{}

func (labelFailWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("injected output failure") }

func TestBulkLabelsStdoutFailureIsNonzero(t *testing.T) {
	test := newAppTest(t, false)
	ctx := "ctx:synthetic-project"
	writeHubConfig(t, test, map[string]string{ctx: test.repository})
	setResponses(t, map[string]string{
		"show:work-a":   `[{"id":"work-a","status":"open","issue_type":"task","labels":["ctx:synthetic-project"]}]`,
		"update:work-a": `[{"id":"work-a","status":"open","issue_type":"task","labels":["ctx:synthetic-project","tag"]}]`,
	})
	var stderr strings.Builder
	test.app.stdout = labelFailWriter{}
	test.app.stderr = &stderr
	if code := test.app.run([]string{"labels", "add", "work-a", "--labels", "tag", "--json"}); code == 0 || !strings.Contains(stderr.String(), "writing bulk label result") {
		t.Fatalf("stdout failure code=%d stderr=%q", code, stderr.String())
	}
}

func TestBulkLabelsSuccessfulNoOps(t *testing.T) {
	for _, testCase := range []struct {
		name, operation, input string
	}{
		{"add existing", "add", "tag"},
		{"remove absent", "remove", "absent"},
		{"replace identical", "replace", "tag"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			test := newAppTest(t, false)
			ctx := "ctx:synthetic-project"
			writeHubConfig(t, test, map[string]string{ctx: test.repository})
			labels := `"ctx:synthetic-project"`
			if testCase.name != "remove absent" {
				labels += `,"tag"`
			}
			setResponses(t, map[string]string{
				"show:work-a":   fmt.Sprintf(`[{"id":"work-a","status":"open","issue_type":"task","labels":[%s]}]`, labels),
				"update:work-a": fmt.Sprintf(`[{"id":"work-a","status":"open","issue_type":"task","labels":[%s]}]`, labels),
			})
			args := []string{"labels", testCase.operation, "work-a", "--labels", testCase.input, "--json"}
			code, stdout, stderr := test.run(args...)
			if code != 0 || stderr != "" {
				t.Fatalf("code=%d stderr=%q stdout=%s", code, stderr, stdout)
			}
			var result bulkLabelResponse
			if err := json.Unmarshal([]byte(stdout), &result); err != nil {
				t.Fatal(err)
			}
			if result.Matched != 1 || result.Applied != 1 || result.Errors != 0 || result.Results[0].Status != "applied" {
				t.Fatalf("no-op result = %#v", result)
			}
		})
	}
}

func TestBulkLabelsPreflightAndDeterministicExecution(t *testing.T) {
	test := newAppTest(t, false)
	ctx := "ctx:synthetic-project"
	writeHubConfig(t, test, map[string]string{ctx: test.repository})
	setResponses(t, map[string]string{
		"show:work-b":   `[{"id":"work-b","status":"open","issue_type":"task","labels":["ctx:synthetic-project","z"]}]`,
		"show:work-a":   `[{"id":"work-a","status":"closed","issue_type":"task","labels":["ctx:synthetic-project"]}]`,
		"update:work-a": `[{"id":"work-a","status":"closed","issue_type":"task","labels":["ctx:synthetic-project","added"]}]`,
		"update:work-b": `[{"id":"work-b","status":"open","issue_type":"task","labels":["ctx:synthetic-project","added","z"]}]`,
	})
	code, stdout, stderr := test.run("labels", "add", "work-b", "work-a", "work-b", "--labels", "added", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q stdout=%s", code, stderr, stdout)
	}
	var result bulkLabelResponse
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result.Matched != 2 || result.Applied != 2 || result.Errors != 0 || len(result.Results) != 2 || result.Results[0].ID != "work-a" || result.Results[0].Labels == nil || !reflect.DeepEqual(*result.Results[0].Labels, []string{"added"}) {
		t.Fatalf("unexpected bulk outcome: %#v", result)
	}
	var updates []childCall
	for _, call := range test.calls() {
		if fakeCommandKey(call.Args) == "update:work-a" || fakeCommandKey(call.Args) == "update:work-b" {
			updates = append(updates, call)
		}
	}
	if len(updates) != 2 || fakeCommandKey(updates[0].Args) != "update:work-a" || !reflect.DeepEqual(updates[0].Args[len(updates[0].Args)-2:], []string{"--add-label", "added"}) {
		t.Fatalf("updates not sorted/label-only: %#v", updates)
	}
}

func TestBulkLabelsPreflightFailurePerformsNoWrites(t *testing.T) {
	test := newAppTest(t, false)
	ctx := "ctx:synthetic-project"
	writeHubConfig(t, test, map[string]string{ctx: test.repository})
	setResponses(t, map[string]string{
		"show:work-a": `[{"id":"different","status":"open","issue_type":"task","labels":["ctx:synthetic-project"]}]`,
	})
	code, stdout, _ := test.run("labels", "replace", "work-a", "--clear", "--json")
	if code == 0 || stdout != "" {
		t.Fatalf("preflight failure code=%d stdout=%q", code, stdout)
	}
	for _, call := range test.calls() {
		if fakeCommandKey(call.Args) == "update:work-a" {
			t.Fatalf("write occurred before all-target preflight: %#v", call)
		}
	}
}

func TestBulkLabelsReplaceUsesOrdinaryOnlyBackendFlag(t *testing.T) {
	test := newAppTest(t, false)
	ctx := "ctx:synthetic-project"
	writeHubConfig(t, test, map[string]string{ctx: test.repository})
	setResponses(t, map[string]string{
		"show:work-a":   `[{"id":"work-a","status":"open","issue_type":"task","labels":["ctx:synthetic-project","old"]}]`,
		"update:work-a": `[{"id":"work-a","status":"open","issue_type":"task","labels":["ctx:synthetic-project"]}]`,
	})
	code, stdout, stderr := test.run("labels", "replace", "work-a", "--clear", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q stdout=%s", code, stderr, stdout)
	}
	for _, call := range test.calls() {
		if fakeCommandKey(call.Args) != "update:work-a" {
			continue
		}
		if !reflect.DeepEqual(call.Args[len(call.Args)-1:], []string{"--set-ordinary-labels="}) {
			t.Fatalf("clear did not use explicit-empty ordinary-only replacement: %#v", call.Args)
		}
		if containsArgument(call.Args, "--set-labels") {
			t.Fatalf("unsafe full-label fallback used: %#v", call.Args)
		}
		return
	}
	t.Fatal("replace issued no backend update")
}

func TestBulkLabelsReportsCompletePartialOutcomesAndSignals(t *testing.T) {
	test := newAppTest(t, false)
	ctx := "ctx:synthetic-project"
	writeHubConfig(t, test, map[string]string{ctx: test.repository})
	setResponses(t, map[string]string{
		"show:work-a":   `[{"id":"work-a","status":"open","issue_type":"task","labels":["ctx:synthetic-project"]}]`,
		"show:work-b":   `[{"id":"work-b","status":"open","issue_type":"task","labels":["ctx:synthetic-project"]}]`,
		"update:work-a": `[{"id":"work-a","status":"open","issue_type":"task","labels":["ctx:synthetic-project","tag"]}]`,
		"update:work-b": `[{"id":"work-b","status":"open","issue_type":"task","labels":["ctx:synthetic-project","tag"]}]`,
	})
	setExitCodes(t, map[string]int{"update:work-a": 9})
	code, stdout, stderr := test.run("labels", "add", "work-a", "work-b", "--labels", "tag", "--json")
	if code != 1 || !strings.Contains(stderr, `"code":"bulk_label_partial_failure"`) {
		t.Fatalf("code=%d stderr=%q stdout=%s", code, stderr, stdout)
	}
	var result bulkLabelResponse
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result.Matched != 2 || result.Applied != 1 || result.Errors != 1 || len(result.Results) != 2 || result.Results[0].Status != "error" || !result.Results[0].OutcomeUnknown || result.Results[0].Labels != nil || result.Results[1].Status != "applied" {
		t.Fatalf("partial outcome is incomplete or misleading: %#v", result)
	}
	if _, err := os.Stat(hub.ChangeSignalPath(test.app.paths)); err != nil {
		t.Fatalf("partial write did not notify Viewer: %v", err)
	}
}

func TestBulkLabelsEpicUsesDirectClosureWithoutRootOrFilters(t *testing.T) {
	test := newAppTest(t, false)
	ctx := "ctx:synthetic-project"
	writeHubConfig(t, test, map[string]string{ctx: test.repository})
	setResponses(t, map[string]string{
		"show:epic-a":               `[{"id":"epic-a","status":"open","issue_type":"epic","labels":[]}]`,
		"epic:child-closure:epic-a": `[{"id":"epic-a"},{"id":"nested-epic"},{"id":"closed-child"},{"id":"wisp-child"},{"id":"hidden-child"}]`,
		"show:nested-epic":          `[{"id":"nested-epic","status":"open","issue_type":"epic","labels":[]}]`,
		"show:closed-child":         `[{"id":"closed-child","status":"closed","issue_type":"task","labels":["ctx:synthetic-project"]}]`,
		"show:wisp-child":           `[{"id":"wisp-child","status":"open","issue_type":"task","labels":["ctx:synthetic-project"],"ephemeral":true}]`,
		"show:hidden-child":         `[{"id":"hidden-child","status":"open","issue_type":"task","labels":["ctx:synthetic-project"],"hidden":true}]`,
		"update:closed-child":       `[{"id":"closed-child","status":"closed","issue_type":"task","labels":["ctx:synthetic-project","tag"]}]`,
		"update:hidden-child":       `[{"id":"hidden-child","status":"open","issue_type":"task","labels":["ctx:synthetic-project","tag"]}]`,
		"update:nested-epic":        `[{"id":"nested-epic","status":"open","issue_type":"epic","labels":["tag"]}]`,
		"update:wisp-child":         `[{"id":"wisp-child","status":"open","issue_type":"task","labels":["ctx:synthetic-project","tag"]}]`,
	})
	code, stdout, stderr := test.run("labels", "add", "--epic", "epic-a", "--labels", "tag", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q stdout=%s", code, stderr, stdout)
	}
	var result bulkLabelResponse
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	want := []string{"closed-child", "hidden-child", "nested-epic", "wisp-child"}
	got := make([]string, len(result.Results))
	for i, row := range result.Results {
		got[i] = row.ID
	}
	if !reflect.DeepEqual(got, want) || result.Matched != len(want) || result.Applied != len(want) {
		t.Fatalf("epic children = %v, outcome=%#v", got, result)
	}
	for _, call := range test.calls() {
		if fakeCommandKey(call.Args) == "epic:child-closure:epic-a" && !reflect.DeepEqual(call.Args[len(call.Args)-3:], []string{"epic", "child-closure", "epic-a"}) {
			t.Fatalf("unexpected epic selection args: %#v", call.Args)
		}
		if fakeCommandKey(call.Args) == "epic:child-closure:nested-epic" {
			t.Fatal("nested epic was recursively traversed")
		}
	}
}
