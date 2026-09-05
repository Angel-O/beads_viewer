package main_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
)

type verticalAssertion uint8

const (
	assertMembershipAdmission verticalAssertion = iota + 1
	assertCreationTargets
	assertRejectedCreationNoWrite
	assertTodoContinuity
	assertEpicCoordination
	assertOrderedCorrection
	assertOriginalLifecycleAttribution
	assertCorrectionCorrelationAttribution
	assertScopeVariants
	assertCanonicalScopeHash
	assertMultiContextDedupe
	assertHiddenBlockerTruth
	assertMultipleEligibleCommits
	assertRejectedCorrelationNoAppend
)

type evidenceEntry struct {
	journey    func(*testing.T)
	assertions []verticalAssertion
}

func evidence(journey func(*testing.T), assertions ...verticalAssertion) evidenceEntry {
	return evidenceEntry{journey: journey, assertions: assertions}
}

// requirementEvidence compile-links every requirement to the exact vertical
// journey and typed assertions that exercise it. Renaming a cited journey or
// adding an unknown assertion breaks compilation or this matrix test.
var requirementEvidence = map[string]evidenceEntry{
	"HCS-INV-001": evidence(TestRealHubCreationLifecycleAndCorrection, assertMembershipAdmission),
	"HCS-INV-002": evidence(TestRealHubCreationLifecycleAndCorrection, assertMembershipAdmission),
	"HCS-INV-003": evidence(TestRealHubCreationLifecycleAndCorrection, assertOrderedCorrection),
	"HCS-INV-004": evidence(TestRealHubCreationLifecycleAndCorrection, assertMembershipAdmission),
	"HCS-INV-005": evidence(TestRealHubCreationLifecycleAndCorrection, assertMembershipAdmission),
	"HCS-INV-006": evidence(TestRealHubCreationLifecycleAndCorrection, assertMembershipAdmission),
	"HCS-INV-007": evidence(TestRealHubScopedRobotReads, assertHiddenBlockerTruth),
	"HCS-INV-008": evidence(TestRealHubScopedRobotReads, assertHiddenBlockerTruth),
	"HCS-CRE-001": evidence(TestRealHubCreationLifecycleAndCorrection, assertCreationTargets),
	"HCS-CRE-002": evidence(TestRealHubCreationLifecycleAndCorrection, assertCreationTargets),
	"HCS-CRE-003": evidence(TestRealHubCreationLifecycleAndCorrection, assertCreationTargets),
	"HCS-CRE-004": evidence(TestRealHubCreationLifecycleAndCorrection, assertCreationTargets),
	"HCS-CRE-005": evidence(TestRealHubCreationLifecycleAndCorrection, assertCreationTargets),
	"HCS-CRE-006": evidence(TestRealHubCreationLifecycleAndCorrection, assertCreationTargets),
	"HCS-CRE-007": evidence(TestRealHubCreationLifecycleAndCorrection, assertRejectedCreationNoWrite),
	"HCS-CRE-008": evidence(TestRealHubCreationLifecycleAndCorrection, assertRejectedCreationNoWrite),
	"HCS-CRE-009": evidence(TestRealHubCreationLifecycleAndCorrection, assertRejectedCreationNoWrite),
	"HCS-LIF-001": evidence(TestRealHubCreationLifecycleAndCorrection, assertTodoContinuity),
	"HCS-LIF-002": evidence(TestRealHubCreationLifecycleAndCorrection, assertTodoContinuity),
	"HCS-LIF-003": evidence(TestRealHubCreationLifecycleAndCorrection, assertOrderedCorrection),
	"HCS-LIF-004": evidence(TestRealHubCreationLifecycleAndCorrection, assertOrderedCorrection),
	"HCS-LIF-005": evidence(TestRealHubCreationLifecycleAndCorrection, assertOriginalLifecycleAttribution),
	"HCS-LIF-006": evidence(TestRealHubCreationLifecycleAndCorrection, assertOrderedCorrection),
	"HCS-LIF-007": evidence(TestRealHubCreationLifecycleAndCorrection, assertOrderedCorrection),
	"HCS-LIF-008": evidence(TestRealHubCreationLifecycleAndCorrection, assertEpicCoordination),
	"HCS-SCP-001": evidence(TestRealHubScopedRobotReads, assertScopeVariants, assertCanonicalScopeHash),
	"HCS-SCP-002": evidence(TestRealHubScopedRobotReads, assertScopeVariants),
	"HCS-SCP-003": evidence(TestRealHubScopedRobotReads, assertScopeVariants),
	"HCS-SCP-004": evidence(TestRealHubScopedRobotReads, assertScopeVariants),
	"HCS-SCP-005": evidence(TestRealHubScopedRobotReads, assertMultiContextDedupe),
	"HCS-COR-001": evidence(TestRealHubCorrelationAndLocalParity, assertMultipleEligibleCommits),
	"HCS-COR-002": evidence(TestRealHubCorrelationAndLocalParity, assertMultipleEligibleCommits, assertRejectedCorrelationNoAppend),
	"HCS-COR-003": evidence(TestRealHubCorrelationAndLocalParity, assertRejectedCorrelationNoAppend),
	"HCS-COR-004": evidence(TestRealHubCorrelationAndLocalParity, assertRejectedCorrelationNoAppend),
	"HCS-COR-005": evidence(TestRealHubCorrelationAndLocalParity, assertMultipleEligibleCommits),
	"HCS-COR-006": evidence(TestRealHubCreationLifecycleAndCorrection, assertCorrectionCorrelationAttribution),
	"HCS-COR-007": evidence(TestRealHubCorrelationAndLocalParity, assertRejectedCorrelationNoAppend),
}

func TestHubContextScopeRequirementEvidence(t *testing.T) {
	expected := []string{
		"HCS-INV-001", "HCS-INV-002", "HCS-INV-003", "HCS-INV-004", "HCS-INV-005", "HCS-INV-006", "HCS-INV-007", "HCS-INV-008",
		"HCS-CRE-001", "HCS-CRE-002", "HCS-CRE-003", "HCS-CRE-004", "HCS-CRE-005", "HCS-CRE-006", "HCS-CRE-007", "HCS-CRE-008", "HCS-CRE-009",
		"HCS-LIF-001", "HCS-LIF-002", "HCS-LIF-003", "HCS-LIF-004", "HCS-LIF-005", "HCS-LIF-006", "HCS-LIF-007", "HCS-LIF-008",
		"HCS-SCP-001", "HCS-SCP-002", "HCS-SCP-003", "HCS-SCP-004", "HCS-SCP-005",
		"HCS-COR-001", "HCS-COR-002", "HCS-COR-003", "HCS-COR-004", "HCS-COR-005", "HCS-COR-006", "HCS-COR-007",
	}
	if len(requirementEvidence) != len(expected) {
		t.Fatalf("requirement evidence entries = %d, want %d", len(requirementEvidence), len(expected))
	}
	seen := make(map[string]bool, len(expected))
	for _, id := range expected {
		if seen[id] {
			t.Fatalf("stable requirement ID is duplicated: %s", id)
		}
		seen[id] = true
		entry, ok := requirementEvidence[id]
		if !ok {
			t.Errorf("%s is missing evidence", id)
			continue
		}
		if entry.journey == nil || len(entry.assertions) == 0 {
			t.Errorf("%s has incomplete vertical evidence", id)
		}
		for _, assertion := range entry.assertions {
			if assertion < assertMembershipAdmission || assertion > assertRejectedCorrelationNoAppend {
				t.Errorf("%s cites unknown assertion %d", id, assertion)
			}
		}
	}
	for id := range requirementEvidence {
		if !seen[id] {
			t.Errorf("evidence matrix contains unknown requirement ID: %s", id)
		}
	}
}

func TestRealHubExistingStoreTodoActivation(t *testing.T) {
	fixture := newRealHubFixtureWithoutBootstrap(t)
	store := filepath.Join(fixture.home, ".local", "share", "beads", "hub", ".beads")
	parent := filepath.Dir(store)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture.runSuccess(t, parent, fixture.bd, "metrics", "off")
	fixture.runSuccess(t, parent, fixture.bd, "init", "--prefix", "qa", "--non-interactive", "--skip-hooks", "--skip-agents")
	existingID := extractCreatedID(t, fixture.runSuccess(t, fixture.outside, fixture.bd, "--db", store, "--json", "create", "Existing work", "--type", "task"))
	marker := filepath.Join(store, "preserve-me")
	if err := os.WriteFile(marker, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}

	before := fixture.issueIDs(t)
	rejection := fixture.runFailure(t, fixture.outside, "wbd", "--json", "create", "Unavailable todo", "--type", "todo", "--contextless")
	if !bytes.Contains(rejection, []byte("run 'wbd bootstrap' to enable it")) {
		t.Fatalf("todo rejection lacks remediation:\n%s", rejection)
	}
	if after := fixture.issueIDs(t); !reflect.DeepEqual(after, before) {
		t.Fatalf("rejected todo changed issues: before=%v after=%v", before, after)
	}
	configPath := filepath.Join(fixture.home, ".config", "bv", "hub.yaml")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("rejected todo registered or configured the Hub: %v", err)
	}

	fixture.runSuccess(t, fixture.outside, fixture.bd, "--db", store, "config", "set", "types.custom", "review")
	activation := fixture.runSuccess(t, fixture.outside, "wbd", "bootstrap")
	if string(activation) != "Hub store ready: todo issue type enabled.\n" {
		t.Fatalf("activation output = %q", activation)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("activation did not ensure Hub config: %v", err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "unchanged" {
		t.Fatalf("activation changed existing store marker: data=%q err=%v", data, err)
	}
	if after := fixture.issueIDs(t); !reflect.DeepEqual(after, before) {
		t.Fatalf("activation changed existing issues: before=%v after=%v", before, after)
	}
	if issue := fixture.show(t, existingID); issue.ID != existingID || issue.IssueType != "task" {
		t.Fatalf("existing issue was not preserved: %#v", issue)
	}
	configOutput := fixture.runSuccess(t, fixture.outside, fixture.bd, "--db", store, "--json", "config", "get", "types.custom")
	var configured struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(configOutput, &configured); err != nil || configured.Value != "review,todo" {
		t.Fatalf("custom types = %q, decode error = %v\n%s", configured.Value, err, configOutput)
	}
	secondActivation := fixture.runSuccess(t, fixture.outside, "wbd", "bootstrap")
	if string(secondActivation) != "Hub store ready: todo issue type already enabled.\n" {
		t.Fatalf("second activation output = %q", secondActivation)
	}
	configOutput = fixture.runSuccess(t, fixture.outside, fixture.bd, "--db", store, "--json", "config", "get", "types.custom")
	if err := json.Unmarshal(configOutput, &configured); err != nil || configured.Value != "review,todo" {
		t.Fatalf("idempotent custom types = %q, decode error = %v\n%s", configured.Value, err, configOutput)
	}
	if _, err := os.Stat(hub.ChangeSignalPath(hub.Paths{Store: store})); !os.IsNotExist(err) {
		t.Fatalf("existing-store bootstrap signaled Viewer: %v", err)
	}

	id := fixture.create(t, fixture.outside, "Activated todo", "--type", "todo", "--contextless", "--labels", "setup-regression")
	issue := fixture.show(t, id)
	if !strings.HasPrefix(existingID, "qa-") || !strings.HasPrefix(id, "qa-") {
		t.Fatalf("activation changed store prefix: existing=%q todo=%q", existingID, id)
	}
	if issue.ID != id || issue.IssueType != "todo" || !slices.Contains(issue.Labels, "setup-regression") {
		t.Fatalf("created todo = %#v", issue)
	}
	for _, label := range issue.Labels {
		if strings.HasPrefix(label, "ctx:") {
			t.Fatalf("contextless todo acquired context label: %#v", issue.Labels)
		}
	}
}

func TestRealHubCreationLifecycleAndCorrection(t *testing.T) {
	fixture := newRealHubFixture(t)
	contexts := fixture.registerRepositories(t, 3)

	defaultWork := fixture.create(t, fixture.repositories[0], "Default work", "--type", "task")
	explicitWork := fixture.create(t, fixture.repositories[0], "Explicit work", "--type", "bug", "--context", contexts[1])
	contextlessTodo := fixture.create(t, fixture.outside, "Contextless todo", "--type", "todo", "--contextless")
	multiTodo := fixture.create(t, fixture.repositories[0], "Shared todo", "--type", "todo", "--context", contexts[0], "--context", contexts[1])
	multiEpic := fixture.create(t, fixture.repositories[0], "Shared epic", "--type", "epic", "--context", contexts[0], "--context", contexts[1])

	fixture.assertContexts(t, defaultWork, contexts[0])
	fixture.assertContexts(t, explicitWork, contexts[1])
	fixture.assertContexts(t, contextlessTodo)
	fixture.assertContexts(t, multiTodo, contexts[0], contexts[1])
	fixture.assertContexts(t, multiEpic, contexts[0], contexts[1])

	before := fixture.issueIDs(t)
	fixture.runFailure(t, fixture.repositories[0], "wbd", "--json", "create", "Invalid cardinality", "--type", "task", "--context", contexts[0], "--context", contexts[1])
	unknown := contexts[0] + "-unregistered"
	fixture.runFailure(t, fixture.repositories[0], "wbd", "--json", "create", "Unknown target", "--type", "task", "--context", unknown)
	fixture.runFailure(t, fixture.outside, "wbd", "--json", "create", "No current target", "--type", "task")
	if after := fixture.issueIDs(t); !reflect.DeepEqual(after, before) {
		t.Fatalf("rejected creation changed issues: before=%v after=%v", before, after)
	}

	result := fixture.create(t, fixture.repositories[0], "Todo result", "--type", "feature", "--context", contexts[0], "--from-todo", contextlessTodo)
	fixture.assertRelation(t, result, contextlessTodo, "discovered-from")
	if todo := fixture.show(t, contextlessTodo); todo.Status == "closed" {
		t.Fatalf("todo %s did not remain durable", contextlessTodo)
	}

	fixture.runSuccess(t, fixture.repositories[0], "wbd", "dep", "add", defaultWork, multiEpic, "--type", "parent-child")
	outsideChild := fixture.create(t, fixture.repositories[2], "Outside child", "--type", "chore")
	fixture.runFailure(t, fixture.repositories[0], "wbd", "--json", "dep", "add", outsideChild, multiEpic, "--type", "parent-child")
	fixture.assertRelation(t, defaultWork, multiEpic, "parent-child")
	fixture.assertNoRelation(t, outsideChild, multiEpic, "parent-child")

	blocker := fixture.create(t, fixture.repositories[0], "Open blocker", "--type", "task")
	original := fixture.create(t, fixture.repositories[0], "Misplaced work", "--type", "task", "--description", "Original audit detail")
	dependent := fixture.create(t, fixture.repositories[0], "Dependent work", "--type", "task")
	fixture.runSuccess(t, fixture.repositories[0], "wbd", "dep", "add", original, blocker)
	fixture.runSuccess(t, fixture.repositories[0], "wbd", "dep", "add", dependent, original)
	fixture.runSuccess(t, fixture.repositories[0], "wbd", "update", original, "--status", "in_progress")
	fixture.runSuccess(t, fixture.repositories[0], "wbd", "link", original, "HEAD")
	replacement := fixture.replace(t, fixture.repositories[0], original, contexts[1])
	if replacement == original {
		t.Fatal("replacement reused the original identity")
	}
	fixture.assertRelation(t, replacement, original, "supersedes")
	fixture.assertRelation(t, replacement, blocker, "blocks")
	fixture.assertRelation(t, dependent, replacement, "blocks")
	fixture.assertContexts(t, original, contexts[0])
	fixture.assertContexts(t, replacement, contexts[1])
	originalIssue := fixture.show(t, original)
	if originalIssue.Status != "closed" || originalIssue.CloseReason != "Superseded by "+replacement {
		t.Fatalf("original outcome = status %q reason %q", originalIssue.Status, originalIssue.CloseReason)
	}
	correlations := fixture.ledger(t)
	if len(correlations) != 1 || correlations[0].BeadID != original {
		t.Fatalf("correction transferred original correlation: %#v", correlations)
	}
	originalEvents := fixture.historyEventTypes(t, original)
	for _, eventType := range []string{"created", "claimed", "closed"} {
		if !slices.Contains(originalEvents, eventType) {
			t.Fatalf("original history lost %q lifecycle event: %v", eventType, originalEvents)
		}
	}
	replacementEvents := fixture.historyEventTypes(t, replacement)
	if slices.Contains(replacementEvents, "claimed") || slices.Contains(replacementEvents, "closed") {
		t.Fatalf("original lifecycle events were rewritten onto replacement: %v", replacementEvents)
	}

	fixture.createLegacyInvalid(t)
	issuesBefore := fixture.issueIDs(t)
	first := fixture.runSuccess(t, fixture.outside, "wbd", "compatibility", "--json")
	second := fixture.runSuccess(t, fixture.outside, "wbd", "compatibility", "--json")
	if string(first) != string(second) || !bytes.Contains(first, []byte(`"code":"invalid_cardinality"`)) {
		t.Fatalf("compatibility output is not deterministic or lacks expected finding:\n%s\n%s", first, second)
	}
	if issuesAfter := fixture.issueIDs(t); !reflect.DeepEqual(issuesAfter, issuesBefore) {
		t.Fatalf("compatibility changed issues: before=%v after=%v", issuesBefore, issuesAfter)
	}
}

func TestRealHubScopedRobotReads(t *testing.T) {
	fixture := newRealHubFixture(t)
	contexts := fixture.registerRepositories(t, 2)
	visible := fixture.create(t, fixture.repositories[0], "Visible blocked work", "--type", "task")
	hidden := fixture.create(t, fixture.repositories[1], "Hidden blocker", "--type", "task")
	shared := fixture.create(t, fixture.repositories[0], "Shared coordination", "--type", "epic", "--context", contexts[0], "--context", contexts[1])
	contextless := fixture.create(t, fixture.outside, "Neutral capture", "--type", "todo", "--contextless")
	fixture.runSuccess(t, fixture.repositories[0], "wbd", "dep", "add", visible, hidden)
	scopeID := extractCreatedID(t, fixture.runSuccess(t, fixture.outside, "wbd", "scope", "create", "scope-qa", "Scoped robot QA", "--activate", "--json"))
	fixture.runSuccess(t, fixture.outside, "wbd", "scope", "add", visible, hidden, shared, contextless, "--scope", scopeID, "--json")

	current := fixture.robot(t, fixture.repositories[0], "wbv", "--hub", "--robot-graph")
	explicit := fixture.robot(t, fixture.repositories[0], "wbv", "--hub", "--context", contexts[1], "--context", contexts[0], "--robot-graph")
	contextlessRead := fixture.robot(t, fixture.outside, "wbv", "--hub", "--contextless", "--robot-graph")
	allItems := fixture.robot(t, fixture.outside, "wbv", "--hub", "--robot-graph")

	repeats := map[string]map[string]any{
		"current":     fixture.robot(t, fixture.repositories[0], "wbv", "--hub", "--robot-graph"),
		"explicit":    fixture.robot(t, fixture.repositories[0], "wbv", "--hub", "--context", contexts[1], "--context", contexts[0], "--robot-graph"),
		"contextless": fixture.robot(t, fixture.outside, "wbv", "--hub", "--contextless", "--robot-graph"),
		"all":         fixture.robot(t, fixture.outside, "wbv", "--hub", "--robot-graph"),
	}
	for name, output := range map[string]map[string]any{"current": current, "explicit": explicit, "contextless": contextlessRead, "all": allItems} {
		if output["data_hash"] != repeats[name]["data_hash"] {
			t.Errorf("%s data hash changed across repeat requests: %v vs %v", name, output["data_hash"], repeats[name]["data_hash"])
		}
	}
	assertRobotIDs(t, current, []string{shared, visible})
	assertRobotIDs(t, explicit, []string{hidden, shared, visible})
	assertRobotIDs(t, contextlessRead, []string{contextless})
	assertRobotIDs(t, allItems, []string{contextless, hidden, shared, visible})
	for name, output := range map[string]map[string]any{"current": current, "explicit": explicit, "contextless": contextlessRead, "all": allItems} {
		scope, ok := output["scope"].(map[string]any)
		if !ok || scope["id"] != scopeID || scope["member_count"] != float64(4) {
			t.Fatalf("%s scope = %#v, want active scope %s with four members", name, output["scope"], scopeID)
		}
	}
	if countRobotID(explicit, shared) != 1 {
		t.Fatalf("multi-context issue %s was not de-duplicated", shared)
	}
	if !robotHasEdge(explicit, visible, hidden, "blocks") {
		t.Fatalf("combined-context graph lacks ordinary edge for %s -> %s", visible, hidden)
	}

	combinedPlan := fixture.robot(t, fixture.repositories[0], "wbv", "--hub", "--context", contexts[1], "--context", contexts[0], "--robot-plan")
	if containsRobotID(combinedPlan, visible) {
		t.Fatalf("hidden blocker made %s ready", visible)
	}
}

func TestRealHubCorrelationAndLocalParity(t *testing.T) {
	fixture := newRealHubFixture(t)
	contexts := fixture.registerRepositories(t, 2)
	work := fixture.create(t, fixture.repositories[0], "Correlated work", "--type", "task")
	todo := fixture.create(t, fixture.repositories[0], "Uncorrelatable todo", "--type", "todo")

	firstSHA := gitOutputCommand(t, fixture.repositories[0], "rev-parse", "HEAD")
	gitCommand(t, fixture.repositories[0], "commit", "--allow-empty", "-m", "second source revision")
	secondSHA := gitOutputCommand(t, fixture.repositories[0], "rev-parse", "HEAD")
	if firstSHA == secondSHA {
		t.Fatal("source commits are not distinct")
	}
	fixture.runSuccess(t, fixture.repositories[0], "wbd", "link", work, firstSHA)
	fixture.runSuccess(t, fixture.repositories[0], "wbd", "link", work, secondSHA)
	records := fixture.ledger(t)
	commits := make(map[string]bool)
	for _, record := range records {
		if record.BeadID != work || record.Context != contexts[0] || len(record.Commit) != 40 && len(record.Commit) != 64 {
			t.Fatalf("invalid eligible correlation record: %#v", record)
		}
		commits[record.Commit] = true
	}
	if len(records) != 2 || !commits[firstSHA] || !commits[secondSHA] {
		t.Fatalf("eligible correlation records = %#v", records)
	}
	fixture.runFailure(t, fixture.repositories[0], "wbd", "link", todo, "HEAD")
	fixture.runFailure(t, fixture.repositories[1], "wbd", "link", work, "HEAD")
	if after := fixture.ledger(t); !reflect.DeepEqual(after, records) {
		t.Fatalf("rejected correlation appended ledger: before=%#v after=%#v", records, after)
	}

	local := fixture.newLocalRepository(t)
	localOutput := fixture.robot(t, local, "wbv", "--local", "--robot-plan")
	if _, exists := localOutput["scope"]; exists || containsJSONKey(localOutput, "boundary_refs") {
		t.Fatalf("local output acquired Hub-only fields: %#v", localOutput)
	}
	direct := fixture.robot(t, local, "bv", "--history-mode", "off", "--robot-plan", "--format", "json")
	if _, exists := direct["scope"]; exists || containsJSONKey(direct, "boundary_refs") {
		t.Fatalf("direct local bv output acquired Hub-only fields: %#v", direct)
	}
}

type realHubFixture struct {
	t            *testing.T
	root         string
	home         string
	outside      string
	bin          string
	bd           string
	repositories []string
	contexts     []string
}

type realIssue struct {
	ID           string         `json:"id"`
	Status       string         `json:"status"`
	IssueType    string         `json:"issue_type"`
	Labels       []string       `json:"labels"`
	CloseReason  string         `json:"close_reason"`
	Dependencies []realRelation `json:"dependencies"`
}

type realRelation struct {
	ID             string `json:"id"`
	DependencyType string `json:"dependency_type"`
}

type ledgerRecord struct {
	BeadID  string `json:"bead_id"`
	Context string `json:"context"`
	Commit  string `json:"commit"`
}

func newRealHubFixture(t *testing.T) *realHubFixture {
	t.Helper()
	f := newRealHubFixtureWithoutBootstrap(t)
	f.runSuccess(t, f.outside, "wbd", "bootstrap")
	return f
}

func newRealHubFixtureWithoutBootstrap(t *testing.T) *realHubFixture {
	t.Helper()
	bd, err := exec.LookPath("bd")
	if err != nil {
		t.Skip("installed supported bd is unavailable")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	outside := filepath.Join(root, "outside")
	for _, directory := range []string{home, outside} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	f := &realHubFixture{t: t, root: root, home: home, outside: outside, bin: filepath.Dir(bvBinaryPath), bd: bd}
	return f
}

func (f *realHubFixture) registerRepositories(t *testing.T, count int) []string {
	t.Helper()
	for index := 0; index < count; index++ {
		repository := filepath.Join(f.root, fmt.Sprintf("repository-%d", index+1))
		if err := os.MkdirAll(repository, 0o700); err != nil {
			t.Fatal(err)
		}
		gitCommand(t, repository, "init")
		gitCommand(t, repository, "config", "user.name", "Hub E2E")
		gitCommand(t, repository, "config", "user.email", "hub-e2e@example.invalid")
		gitCommand(t, repository, "commit", "--allow-empty", "-m", "initial")
		gitCommand(t, repository, "remote", "add", "origin", fmt.Sprintf("https://example.invalid/team/repository-%d.git", index+1))
		output := f.runSuccess(t, repository, "wbd", "register")
		contextID, _, ok := strings.Cut(strings.TrimSpace(string(output)), "\t")
		if !ok || contextID == "" {
			t.Fatalf("register output = %q", output)
		}
		f.repositories = append(f.repositories, repository)
		f.contexts = append(f.contexts, contextID)
	}
	return append([]string(nil), f.contexts...)
}

func (f *realHubFixture) create(t *testing.T, directory, title string, arguments ...string) string {
	t.Helper()
	args := []string{"--json", "create", title}
	args = append(args, arguments...)
	return extractCreatedID(t, f.runSuccess(t, directory, "wbd", args...))
}

func (f *realHubFixture) replace(t *testing.T, directory, original, contextID string) string {
	t.Helper()
	output := f.runSuccess(t, directory, "wbd", "--json", "replace", original, "--context", contextID)
	var result struct {
		ReplacementID string `json:"replacement_id"`
	}
	if err := json.Unmarshal(output, &result); err != nil || result.ReplacementID == "" {
		t.Fatalf("decode replacement: %v\n%s", err, output)
	}
	return result.ReplacementID
}

func (f *realHubFixture) show(t *testing.T, id string) realIssue {
	t.Helper()
	output := f.runSuccess(t, f.outside, "wbd", "show", id, "--json")
	var issues []realIssue
	if err := json.Unmarshal(output, &issues); err != nil || len(issues) != 1 {
		t.Fatalf("decode issue %s: %v\n%s", id, err, output)
	}
	return issues[0]
}

func (f *realHubFixture) assertContexts(t *testing.T, id string, want ...string) {
	t.Helper()
	issue := f.show(t, id)
	var got []string
	for _, label := range issue.Labels {
		if strings.HasPrefix(label, "ctx:") {
			got = append(got, label)
		}
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issue %s contexts = %v, want %v", id, got, want)
	}
}

func (f *realHubFixture) assertRelation(t *testing.T, from, to, relationType string) {
	t.Helper()
	for _, relation := range f.show(t, from).Dependencies {
		if relation.ID == to && relation.DependencyType == relationType {
			return
		}
	}
	t.Fatalf("issue %s lacks %s relation to %s", from, relationType, to)
}

func (f *realHubFixture) assertNoRelation(t *testing.T, from, to, relationType string) {
	t.Helper()
	for _, relation := range f.show(t, from).Dependencies {
		if relation.ID == to && relation.DependencyType == relationType {
			t.Fatalf("issue %s unexpectedly has %s relation to %s", from, relationType, to)
		}
	}
}

func (f *realHubFixture) issueIDs(t *testing.T) []string {
	t.Helper()
	output := f.runSuccess(t, f.outside, "wbd", "list", "--all-contexts", "--json", "--limit", "1000")
	var issues []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(output, &issues); err != nil {
		t.Fatalf("decode issue list: %v\n%s", err, output)
	}
	ids := make([]string, 0, len(issues))
	for _, issue := range issues {
		ids = append(ids, issue.ID)
	}
	sort.Strings(ids)
	return ids
}

func (f *realHubFixture) createLegacyInvalid(t *testing.T) {
	t.Helper()
	store := filepath.Join(f.home, ".local", "share", "beads", "hub", ".beads")
	f.runSuccess(t, f.outside, f.bd, "--db", store, "--json", "create", "Legacy invalid", "--type", "task")
}

func (f *realHubFixture) ledger(t *testing.T) []ledgerRecord {
	t.Helper()
	path := filepath.Join(f.home, ".local", "share", "beads", "hub", "correlations.jsonl")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var records []ledgerRecord
	decoder := json.NewDecoder(bytes.NewReader(data))
	for decoder.More() {
		var record ledgerRecord
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].BeadID+records[i].Context+records[i].Commit < records[j].BeadID+records[j].Context+records[j].Commit
	})
	return records
}

func (f *realHubFixture) robot(t *testing.T, directory, command string, arguments ...string) map[string]any {
	t.Helper()
	output := f.runSuccess(t, directory, command, arguments...)
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode %s robot output: %v\n%s", command, err, output)
	}
	return result
}

func (f *realHubFixture) historyEventTypes(t *testing.T, id string) []string {
	t.Helper()
	config := filepath.Join(f.home, ".config", "bv", "hub.yaml")
	output := f.robot(t, f.outside, "bv", "--bead-history", id, "--history-mode", "external", "--hub-config", config)
	histories, ok := output["histories"].(map[string]any)
	if !ok {
		t.Fatalf("history output lacks histories: %#v", output)
	}
	history, ok := histories[id].(map[string]any)
	if !ok {
		t.Fatalf("history output lacks original identity %s: %#v", id, histories)
	}
	events, ok := history["events"].([]any)
	if !ok {
		t.Fatalf("history for %s lacks events: %#v", id, history)
	}
	types := make([]string, 0, len(events))
	for _, raw := range events {
		event, ok := raw.(map[string]any)
		if !ok || event["bead_id"] != id {
			t.Fatalf("history event is not attributed to %s: %#v", id, raw)
		}
		eventType, ok := event["event_type"].(string)
		if !ok || eventType == "" {
			t.Fatalf("history event lacks type: %#v", event)
		}
		types = append(types, eventType)
	}
	return types
}

func (f *realHubFixture) newLocalRepository(t *testing.T) string {
	t.Helper()
	repository := filepath.Join(f.root, "local")
	if err := os.MkdirAll(filepath.Join(repository, ".beads"), 0o700); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, repository, "init")
	issue := `{"id":"local-1","title":"Local work","status":"open","priority":2,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(filepath.Join(repository, ".beads", "issues.jsonl"), []byte(issue), 0o600); err != nil {
		t.Fatal(err)
	}
	return repository
}

func (f *realHubFixture) runSuccess(t *testing.T, directory, command string, arguments ...string) []byte {
	t.Helper()
	output, err := f.run(directory, command, arguments...)
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", command, strings.Join(arguments, " "), err, output)
	}
	return output
}

func (f *realHubFixture) runFailure(t *testing.T, directory, command string, arguments ...string) []byte {
	t.Helper()
	output, err := f.run(directory, command, arguments...)
	if err == nil {
		t.Fatalf("%s %s unexpectedly succeeded:\n%s", command, strings.Join(arguments, " "), output)
	}
	return output
}

func (f *realHubFixture) run(directory, command string, arguments ...string) ([]byte, error) {
	path := command
	if command == "wbd" {
		path = wbdBinaryPath
	} else if command == "wbv" {
		path = wbvBinaryPath
	} else if command == "bv" {
		path = bvBinaryPath
	}
	cmd := exec.Command(path, arguments...)
	cmd.Dir = directory
	cmd.Env = f.environment()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return append(stdout.Bytes(), stderr.Bytes()...), err
	}
	return stdout.Bytes(), nil
}

func (f *realHubFixture) environment() []string {
	blocked := []string{
		"HOME=", "XDG_CONFIG_HOME=", "BEADS_DIR=", "BEADS_DB=", "BD_DB=", "BD_GLOBAL=",
		"BEADS_DOLT_", "BV_WBV_HUB_SCOPE=", "BV_HUB_CHANGE_SIGNAL=", "BV_NO_BROWSER=", "BV_TEST_MODE=",
	}
	environment := make([]string, 0, len(os.Environ())+8)
	for _, entry := range os.Environ() {
		skip := false
		for _, prefix := range blocked {
			if strings.HasPrefix(entry, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			environment = append(environment, entry)
		}
	}
	path := f.bin + string(os.PathListSeparator) + filepath.Dir(f.bd) + string(os.PathListSeparator) + os.Getenv("PATH")
	return append(environment,
		"HOME="+f.home,
		"XDG_CONFIG_HOME="+filepath.Join(f.home, ".config"),
		"PATH="+path,
		"BV_NO_BROWSER=1",
		"BV_TEST_MODE=1",
		"BV_NO_SAVED_CONFIG=1",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
}

func extractCreatedID(t *testing.T, output []byte) string {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(output, &object); err == nil {
		for _, key := range []string{"id", "issue_id"} {
			if id, ok := object[key].(string); ok && id != "" {
				return id
			}
		}
	}
	var objects []map[string]any
	if err := json.Unmarshal(output, &objects); err == nil && len(objects) == 1 {
		if id, ok := objects[0]["id"].(string); ok && id != "" {
			return id
		}
	}
	t.Fatalf("creation output omitted issue ID:\n%s", output)
	return ""
}

func assertRobotIDs(t *testing.T, output map[string]any, want []string) {
	t.Helper()
	got := collectRobotIDs(output)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("robot IDs = %v, want %v", got, want)
	}
}

func collectRobotIDs(value any) []string {
	seen := make(map[string]bool)
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			if id, ok := typed["id"].(string); ok && id != "" {
				seen[id] = true
			}
			for key, child := range typed {
				if key != "boundary_refs" && key != "scope" {
					walk(child)
				}
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func countRobotID(output map[string]any, id string) int {
	data, _ := json.Marshal(output)
	return strings.Count(string(data), `"id":"`+id+`"`)
}

func containsRobotID(output map[string]any, id string) bool {
	for _, candidate := range collectRobotIDs(output) {
		if candidate == id {
			return true
		}
	}
	return false
}

func robotHasEdge(output map[string]any, from, to, relationType string) bool {
	adjacency, ok := output["adjacency"].(map[string]any)
	if !ok {
		return false
	}
	edges, ok := adjacency["edges"].([]any)
	if !ok {
		return false
	}
	for _, raw := range edges {
		edge, ok := raw.(map[string]any)
		if ok && edge["from"] == from && edge["to"] == to && edge["type"] == relationType {
			return true
		}
	}
	return false
}

func containsJSONKey(value any, wanted string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if _, exists := typed[wanted]; exists {
			return true
		}
		for _, child := range typed {
			if containsJSONKey(child, wanted) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsJSONKey(child, wanted) {
				return true
			}
		}
	}
	return false
}

func gitCommand(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
}

func gitOutputCommand(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(arguments, " "), err)
	}
	return strings.TrimSpace(string(output))
}
