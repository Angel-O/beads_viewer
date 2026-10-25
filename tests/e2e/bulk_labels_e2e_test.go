package main_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type bulkLabelOutcome struct {
	Operation string `json:"operation"`
	Matched   int    `json:"matched"`
	Applied   int    `json:"applied"`
	Errors    int    `json:"errors"`
	Results   []struct {
		ID     string   `json:"id"`
		Status string   `json:"status"`
		Labels []string `json:"labels"`
	} `json:"results"`
}

func TestRealHubBulkLabelsPersistAndPreserveSelection(t *testing.T) {
	fixture := newBulkLabelsRealHubFixture(t)
	contexts := fixture.registerRepositories(t, 2)

	first := fixture.create(t, fixture.repositories[0], "Synthetic first", "--type", "task", "--labels", "keep,remove-me")
	second := fixture.create(t, fixture.repositories[1], "Synthetic second", "--type", "bug", "--labels", "keep,remove-me")
	contextless := fixture.create(t, fixture.outside, "Synthetic contextless", "--type", "todo", "--contextless", "--labels", "keep,remove-me")
	parent := fixture.create(t, fixture.repositories[0], "Synthetic epic", "--type", "epic")
	direct := fixture.create(t, fixture.repositories[1], "Synthetic closed child", "--type", "task", "--labels", "keep,remove-me")
	secondDirect := fixture.create(t, fixture.repositories[0], "Synthetic direct child", "--type", "task", "--labels", "keep,remove-me")
	nested := fixture.create(t, fixture.repositories[0], "Synthetic legacy nested epic", "--type", "epic", "--labels", "keep,remove-me")
	grandchild := fixture.create(t, fixture.repositories[0], "Synthetic nested descendant", "--type", "task", "--labels", "untouched")
	fixture.runSuccess(t, fixture.repositories[0], "wbd", "dep", "add", direct, parent, "--type", "parent-child")
	fixture.runSuccess(t, fixture.repositories[0], "wbd", "dep", "add", secondDirect, parent, "--type", "parent-child")
	fixture.runSuccess(t, fixture.repositories[1], "wbd", "close", direct, "--reason", "synthetic fixture")
	fixture.seedParentChild(t, nested, parent)
	fixture.seedParentChild(t, grandchild, nested)
	wisp := extractCreatedID(t, fixture.runSuccess(t, fixture.outside, fixture.bd, "--db", fixture.storePath(), "--json", "create", "Synthetic task wisp", "--type", "task", "--ephemeral", "--labels", contexts[0]+",keep,remove-me"))
	fixture.seedParentChild(t, wisp, parent)
	unsupported := extractCreatedID(t, fixture.runSuccess(t, fixture.outside, fixture.bd, "--db", fixture.storePath(), "--json", "create", "Synthetic unsupported record", "--type", "spike", "--labels", contexts[0]))

	fixture.runSuccess(t, fixture.outside, "wbd", "scope", "create", "synthetic-active", "Synthetic active scope", "--activate", "--json")
	if active := fixture.runSuccess(t, fixture.outside, "wbd", "scope", "active", "--json"); !strings.Contains(string(active), "synthetic-active") {
		t.Fatalf("active scope did not resolve to the synthetic scope: %s", active)
	}
	scope := fixture.runSuccess(t, fixture.outside, "wbd", "scope", "show", "synthetic-active", "--json")
	for _, id := range []string{first, second, contextless, parent, direct, secondDirect, nested, grandchild, wisp} {
		if strings.Contains(string(scope), id) {
			t.Fatalf("fixture target %s unexpectedly belongs to the active scope: %s", id, scope)
		}
	}

	before := map[string]realIssue{}
	for _, id := range []string{first, second, contextless, parent, direct, secondDirect, nested, grandchild, wisp} {
		before[id] = fixture.show(t, id)
	}
	if !containsLabel(before[first].Labels, contexts[0]) || !containsLabel(before[second].Labels, contexts[1]) {
		t.Fatal("fixture issues are missing their stored repository contexts")
	}

	// Arbitrary selectors are cross-context and include an eligible contextless todo.
	for _, step := range []struct {
		args    []string
		targets []string
		labels  []string
	}{
		{[]string{"add", first, second, contextless, "--labels", "bulk-one, bulk-two", "--json"}, []string{first, second, contextless}, []string{"bulk-one", "bulk-two", "keep", "remove-me"}},
		{[]string{"add", first, second, contextless, "--labels", "bulk-one, bulk-two", "--json"}, []string{first, second, contextless}, []string{"bulk-one", "bulk-two", "keep", "remove-me"}},
		{[]string{"remove", first, second, contextless, "--labels", "remove-me", "--json"}, []string{first, second, contextless}, []string{"bulk-one", "bulk-two", "keep"}},
		{[]string{"remove", first, second, contextless, "--labels", "remove-me", "--json"}, []string{first, second, contextless}, []string{"bulk-one", "bulk-two", "keep"}},
		{[]string{"replace", first, second, contextless, "--labels", "replacement", "--json"}, []string{first, second, contextless}, []string{"replacement"}},
		{[]string{"replace", first, second, contextless, "--labels", "replacement", "--json"}, []string{first, second, contextless}, []string{"replacement"}},
		{[]string{"replace", first, second, contextless, "--clear", "--json"}, []string{first, second, contextless}, []string{}},
		{[]string{"replace", first, second, contextless, "--clear", "--json"}, []string{first, second, contextless}, []string{}},
	} {
		outcome := fixture.bulkLabels(t, fixture.outside, step.args...)
		if outcome.Matched != 3 || outcome.Applied != 3 || outcome.Errors != 0 {
			t.Fatalf("arbitrary %s outcome = %#v", step.args[0], outcome)
		}
		assertSortedOutcomes(t, outcome, step.labels)
		for _, id := range step.targets {
			got := fixture.show(t, id)
			if !reflect.DeepEqual(sortedLabels(ordinaryLabelsForTest(got.Labels)), sortedLabels(step.labels)) || !sameContextLabels(got.Labels, before[id].Labels) {
				t.Fatalf("explicit %s persistence for %s: labels=%v contexts=%v", step.args[0], id, got.Labels, contextLabelsForTest(got.Labels))
			}
		}
	}
	for id, issue := range before {
		got := fixture.show(t, id)
		if got.ID != issue.ID || got.Title != issue.Title || got.Priority != issue.Priority || got.Description != issue.Description || got.Assignee != issue.Assignee || got.IssueType != issue.IssueType || got.Status != issue.Status || got.CloseReason != issue.CloseReason || !reflect.DeepEqual(got.Dependencies, issue.Dependencies) {
			t.Fatalf("bulk labels changed identity, unrelated fields, or relationships for %s: before=%#v after=%#v", id, issue, got)
		}
		if id == first || id == second || id == contextless {
			if !sameContextLabels(got.Labels, issue.Labels) || len(ordinaryLabelsForTest(got.Labels)) != 0 {
				t.Fatalf("clear failed or changed context labels for %s: before=%v after=%v", id, issue.Labels, got.Labels)
			}
		} else if !reflect.DeepEqual(sortedLabels(got.Labels), sortedLabels(issue.Labels)) {
			t.Fatalf("explicit selection changed unselected issue %s labels: before=%v after=%v", id, issue.Labels, got.Labels)
		}
	}

	// The legacy nested-epic edge and task wisp are seeded through the same backend
	// used by wbd; normal wbd attachment policy remains unchanged.
	for _, step := range []struct {
		args    []string
		targets []string
		labels  []string
	}{
		{[]string{"add", "--epic", parent, "--labels", "epic-added", "--json"}, []string{direct, secondDirect, nested, wisp}, []string{"epic-added", "keep", "remove-me"}},
		{[]string{"add", "--epic", parent, "--labels", "epic-added", "--json"}, []string{direct, secondDirect, nested, wisp}, []string{"epic-added", "keep", "remove-me"}},
		{[]string{"remove", "--epic", parent, "--labels", "remove-me", "--json"}, []string{direct, secondDirect, nested, wisp}, []string{"epic-added", "keep"}},
		{[]string{"remove", "--epic", parent, "--labels", "remove-me", "--json"}, []string{direct, secondDirect, nested, wisp}, []string{"epic-added", "keep"}},
		{[]string{"replace", "--epic", parent, "--labels", "epic-replaced", "--json"}, []string{direct, secondDirect, nested, wisp}, []string{"epic-replaced"}},
		{[]string{"replace", "--epic", parent, "--labels", "epic-replaced", "--json"}, []string{direct, secondDirect, nested, wisp}, []string{"epic-replaced"}},
		{[]string{"replace", "--epic", parent, "--clear", "--json"}, []string{direct, secondDirect, nested, wisp}, []string{}},
		{[]string{"replace", "--epic", parent, "--clear", "--json"}, []string{direct, secondDirect, nested, wisp}, []string{}},
	} {
		outcome := fixture.bulkLabels(t, fixture.outside, step.args...)
		if outcome.Matched != 4 || outcome.Applied != 4 || outcome.Errors != 0 {
			t.Fatalf("epic %s outcome = %#v", step.args[0], outcome)
		}
		assertSortedOutcomes(t, outcome, step.labels)
		for _, id := range step.targets {
			got := fixture.show(t, id)
			if !reflect.DeepEqual(sortedLabels(ordinaryLabelsForTest(got.Labels)), sortedLabels(step.labels)) || !sameContextLabels(got.Labels, before[id].Labels) {
				t.Fatalf("epic %s persistence for %s: labels=%v contexts=%v", step.args[0], id, got.Labels, contextLabelsForTest(got.Labels))
			}
		}
	}
	if !reflect.DeepEqual(sortedLabels(fixture.show(t, parent).Labels), sortedLabels(before[parent].Labels)) {
		t.Fatal("selected epic root was mutated")
	}
	for _, id := range []string{direct, secondDirect, nested, wisp} {
		got := fixture.show(t, id)
		if !sameContextLabels(got.Labels, before[id].Labels) || len(ordinaryLabelsForTest(got.Labels)) != 0 {
			t.Fatalf("epic clear left ordinary labels on direct child %s", id)
		}
	}

	emptyEpic := fixture.create(t, fixture.repositories[0], "Synthetic empty epic", "--type", "epic")
	empty := fixture.bulkLabels(t, fixture.outside, "replace", "--epic", emptyEpic, "--clear", "--json")
	if empty.Matched != 0 || empty.Applied != 0 || empty.Errors != 0 || len(empty.Results) != 0 {
		t.Fatalf("empty epic outcome = %#v", empty)
	}

	// Protected-label and invalid-target preflight failures must not modify any target.
	beforeFirst := fixture.show(t, first)
	fixture.runFailure(t, fixture.outside, "wbd", "labels", "remove", first, "--labels", contexts[0], "--json")
	fixture.runFailure(t, fixture.outside, "wbd", "labels", "add", first, "missing-synthetic-id", "--labels", "must-not-write", "--json")
	fixture.runFailure(t, fixture.outside, "wbd", "labels", "add", first, unsupported, "--labels", "must-not-write", "--json")
	if got := fixture.show(t, first); !reflect.DeepEqual(sortedLabels(got.Labels), sortedLabels(beforeFirst.Labels)) {
		t.Fatalf("preflight failure wrote labels: before=%v after=%v", beforeFirst.Labels, got.Labels)
	}
	for _, id := range []string{parent, direct, secondDirect, nested, grandchild, wisp} {
		got, want := fixture.show(t, id), before[id]
		if got.ID != want.ID || got.Title != want.Title || got.Priority != want.Priority || got.Description != want.Description || got.Assignee != want.Assignee || got.IssueType != want.IssueType || got.Status != want.Status || got.CloseReason != want.CloseReason || !reflect.DeepEqual(got.Dependencies, want.Dependencies) || !sameContextLabels(got.Labels, want.Labels) {
			t.Fatalf("epic mutation changed non-label state or preserved context on %s: before=%#v after=%#v", id, want, got)
		}
	}
	if !reflect.DeepEqual(sortedLabels(fixture.show(t, grandchild).Labels), sortedLabels(before[grandchild].Labels)) {
		t.Fatal("epic selection traversed beneath the nested epic")
	}
	if got := fixture.show(t, unsupported); !reflect.DeepEqual(sortedLabels(got.Labels), sortedLabels([]string{contexts[0]})) {
		t.Fatalf("unsupported record changed during rejected selection: %v", got.Labels)
	}
}

func TestBulkLabelsUnsupportedReplacementDoesNotFallBack(t *testing.T) {
	fixture := newBulkLabelsRealHubFixture(t)
	contexts := fixture.registerRepositories(t, 1)
	target := fixture.create(t, fixture.repositories[0], "Synthetic unsupported-backend target", "--type", "task", "--labels", "keep")
	before := fixture.show(t, target)
	shimDir := filepath.Join(fixture.root, "unsupported-backend-bin")
	if err := os.MkdirAll(shimDir, 0o700); err != nil {
		t.Fatal(err)
	}
	unsupportedCall := filepath.Join(fixture.root, "unsupported-replacement-called")
	fallbackCall := filepath.Join(fixture.root, "unsafe-full-label-fallback-called")
	script := fmt.Sprintf(`#!/bin/sh
case " $* " in
  *--set-ordinary-labels*) printf 'unsupported\n' >> %q; echo 'Error: unknown flag: --set-ordinary-labels' >&2; exit 2 ;;
  *--set-labels*) printf 'fallback\n' >> %q; echo 'full-label fallback invoked' >&2; exit 3 ;;
esac
exec %s "$@"
`, unsupportedCall, fallbackCall, shellQuote(fixture.bd))
	if err := os.WriteFile(filepath.Join(shimDir, "bd"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	fixture.bin = shimDir
	output, err := fixture.run(fixture.outside, "wbd", "labels", "replace", target, "--labels", "replacement", "--json")
	if err == nil || !strings.Contains(string(output), `"outcome_unknown":true`) || !strings.Contains(string(output), `"code":"bulk_label_partial_failure"`) {
		t.Fatalf("unsupported replacement result: err=%v output=%s", err, output)
	}
	if _, err := os.Stat(unsupportedCall); err != nil {
		t.Fatalf("test backend shim did not receive replacement call: %v", err)
	}
	if _, err := os.Stat(fallbackCall); !os.IsNotExist(err) {
		t.Fatalf("unsafe full-label fallback was invoked: stat err=%v", err)
	}
	if after := fixture.show(t, target); !reflect.DeepEqual(sortedLabels(after.Labels), sortedLabels(before.Labels)) || !containsLabel(after.Labels, contexts[0]) {
		t.Fatalf("unsupported replacement changed stored labels: before=%v after=%v", before.Labels, after.Labels)
	}
}

func newBulkLabelsRealHubFixture(t *testing.T) *realHubFixture {
	t.Helper()
	path := strings.TrimSpace(os.Getenv("BV_E2E_BD"))
	if path == "" {
		var err error
		path, err = exec.LookPath("bd")
		if err != nil {
			t.Fatalf("supported backend bd is unavailable: %v", err)
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		t.Fatalf("read approved backend %s: %v", absolute, err)
	}
	digest := sha256.Sum256(data)
	actualDigest := hex.EncodeToString(digest[:])
	t.Logf("bulk-label backend path=%s sha256=%s", absolute, actualDigest)
	if expected := strings.TrimSpace(os.Getenv("BV_E2E_BD_SHA256")); expected != "" && actualDigest != strings.ToLower(expected) {
		t.Fatalf("backend SHA256 = %s, want BV_E2E_BD_SHA256 %s", actualDigest, expected)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	outside := filepath.Join(root, "outside")
	for _, directory := range []string{home, outside} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(absolute, filepath.Join(bin, "bd")); err != nil {
		t.Fatalf("route isolated wbd calls to selected backend: %v", err)
	}
	fixture := &realHubFixture{t: t, root: root, home: home, outside: outside, bin: bin, bd: absolute}
	fixture.runSuccess(t, fixture.outside, "wbd", "bootstrap")
	return fixture
}

func (f *realHubFixture) storePath() string {
	return filepath.Join(f.home, ".local", "share", "beads", "hub", ".beads")
}

func (f *realHubFixture) seedParentChild(t *testing.T, child, parent string) {
	t.Helper()
	f.runSuccess(t, f.outside, f.bd, "--db", f.storePath(), "dep", "add", child, parent, "--type", "parent-child")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func (f *realHubFixture) bulkLabels(t *testing.T, directory string, arguments ...string) bulkLabelOutcome {
	t.Helper()
	output := f.runSuccess(t, directory, "wbd", append([]string{"labels"}, arguments...)...)
	var result bulkLabelOutcome
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode bulk labels output: %v\n%s", err, output)
	}
	return result
}

func assertSortedOutcomes(t *testing.T, outcome bulkLabelOutcome, wantLabels []string) {
	t.Helper()
	ids := make([]string, len(outcome.Results))
	for i, result := range outcome.Results {
		ids[i] = result.ID
		if result.Status != "applied" {
			t.Fatalf("result for %s has status %q", result.ID, result.Status)
		}
		if !reflect.DeepEqual(result.Labels, wantLabels) {
			t.Fatalf("post-state labels for %s = %v, want %v", result.ID, result.Labels, wantLabels)
		}
	}
	if !sort.StringsAreSorted(ids) {
		t.Fatalf("result IDs are not sorted: %v", ids)
	}
}

func containsLabel(labels []string, label string) bool {
	for _, candidate := range labels {
		if candidate == label {
			return true
		}
	}
	return false
}

func sameContextLabels(left, right []string) bool {
	return reflect.DeepEqual(contextLabelsForTest(left), contextLabelsForTest(right))
}

func contextLabelsForTest(labels []string) []string {
	result := make([]string, 0)
	for _, label := range labels {
		if strings.HasPrefix(label, "ctx:") {
			result = append(result, label)
		}
	}
	return sortedLabels(result)
}

func ordinaryLabelsForTest(labels []string) []string {
	result := make([]string, 0)
	for _, label := range labels {
		if !strings.HasPrefix(label, "ctx:") {
			result = append(result, label)
		}
	}
	return sortedLabels(result)
}

func sortedLabels(labels []string) []string {
	result := append([]string(nil), labels...)
	sort.Strings(result)
	return result
}
