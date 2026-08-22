package main_test

import (
	"bytes"
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

// requirementEvidence keeps the accepted 37-requirement baseline auditable
// without duplicating parser and renderer permutations already covered by the
// focused package tests named below.
var requirementEvidence = map[string]string{
	"HCS-INV-001": "TestRealHubCreationLifecycleAndCorrection; TestCreateTargetingAndAdmission",
	"HCS-INV-002": "TestRealHubCreationLifecycleAndCorrection; TestAdmitIssue",
	"HCS-INV-003": "TestRealHubCreationLifecycleAndCorrection; TestReplaceCopiesOpenBlockingContinuityThenCloses",
	"HCS-INV-004": "TestRealHubCreationLifecycleAndCorrection; TestAdmitIssue",
	"HCS-INV-005": "TestRealHubCreationLifecycleAndCorrection; TestAdmitIssue",
	"HCS-INV-006": "TestRealHubCreationLifecycleAndCorrection; TestAdmitIssue",
	"HCS-INV-007": "TestRealHubScopedRobotReads; TestHubScopeProjectionCandidateSemanticsAndBoundaryReferences",
	"HCS-INV-008": "TestRealHubScopedRobotReads; TestHubScopeProjectionCandidateSemanticsAndBoundaryReferences",
	"HCS-CRE-001": "TestRealHubCreationLifecycleAndCorrection; TestCreateRegistersAndForwardsExactArgumentsAndEnvironment",
	"HCS-CRE-002": "TestRealHubCreationLifecycleAndCorrection; TestCreateTargetingAndAdmission",
	"HCS-CRE-003": "TestRealHubCreationLifecycleAndCorrection; TestCreateTargetingAndAdmission",
	"HCS-CRE-004": "TestRealHubCreationLifecycleAndCorrection; TestAdmitIssue",
	"HCS-CRE-005": "TestRealHubCreationLifecycleAndCorrection; TestAdmitIssue",
	"HCS-CRE-006": "TestRealHubCreationLifecycleAndCorrection; TestAdmitIssue",
	"HCS-CRE-007": "TestRealHubCreationLifecycleAndCorrection; TestAdmitIssue",
	"HCS-CRE-008": "TestRealHubCreationLifecycleAndCorrection; TestCreateTargetingAndAdmission",
	"HCS-CRE-009": "TestRealHubCreationLifecycleAndCorrection; TestCreateTargetingAndAdmission",
	"HCS-LIF-001": "TestRealHubCreationLifecycleAndCorrection; TestCreateFromTodoUsesOneGraphMutation",
	"HCS-LIF-002": "TestRealHubCreationLifecycleAndCorrection; TestValidateTodoResult",
	"HCS-LIF-003": "TestRealHubCreationLifecycleAndCorrection; TestValidateSupersession",
	"HCS-LIF-004": "TestRealHubCreationLifecycleAndCorrection; TestReplaceCopiesOpenBlockingContinuityThenCloses",
	"HCS-LIF-005": "TestRealHubCreationLifecycleAndCorrection; TestReplaceCopiesOpenBlockingContinuityThenCloses",
	"HCS-LIF-006": "TestRealHubCreationLifecycleAndCorrection; TestReplaceCloseFailureNamesPersistedReplacement",
	"HCS-LIF-007": "TestRealHubCreationLifecycleAndCorrection; TestValidateSupersession",
	"HCS-LIF-008": "TestRealHubCreationLifecycleAndCorrection; TestEpicParentAndSupersededReopenPrevalidation",
	"HCS-SCP-001": "TestRealHubScopedRobotReads; TestHubRobotScopeDefaultsAndContextless",
	"HCS-SCP-002": "TestRealHubScopedRobotReads; TestHubRobotScopeRouting",
	"HCS-SCP-003": "TestRealHubScopedRobotReads; TestHubScopeProjectionVariantsAndCanonicalHash",
	"HCS-SCP-004": "TestRealHubScopedRobotReads; TestHubScopeProjectionCandidateSemanticsAndBoundaryReferences",
	"HCS-SCP-005": "TestRealHubScopedRobotReads; TestHubScopeProjectionCandidateSemanticsAndBoundaryReferences",
	"HCS-COR-001": "TestRealHubCorrelationAndLocalParity; TestAddExternalCorrelation",
	"HCS-COR-002": "TestRealHubCorrelationAndLocalParity; TestExternalCorrelationEligibility",
	"HCS-COR-003": "TestRealHubCorrelationAndLocalParity; TestExternalCorrelationEligibility",
	"HCS-COR-004": "TestRealHubCorrelationAndLocalParity; TestExternalCorrelationEligibility",
	"HCS-COR-005": "TestRealHubCorrelationAndLocalParity; TestAddExternalCorrelation",
	"HCS-COR-006": "TestRealHubCreationLifecycleAndCorrection; TestReplaceCopiesOpenBlockingContinuityThenCloses",
	"HCS-COR-007": "TestRealHubCorrelationAndLocalParity; TestLinkRejectsTodoBeforeCorrelation",
}

func TestHubContextScopeRequirementEvidence(t *testing.T) {
	if len(requirementEvidence) != 37 {
		t.Fatalf("requirement evidence entries = %d, want 37", len(requirementEvidence))
	}
	for id, evidence := range requirementEvidence {
		if evidence == "" {
			t.Errorf("%s has no evidence", id)
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
	original := fixture.create(t, fixture.repositories[0], "Misplaced work", "--type", "task")
	dependent := fixture.create(t, fixture.repositories[0], "Dependent work", "--type", "task")
	fixture.runSuccess(t, fixture.repositories[0], "wbd", "dep", "add", original, blocker)
	fixture.runSuccess(t, fixture.repositories[0], "wbd", "dep", "add", dependent, original)
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

	current := fixture.robot(t, fixture.repositories[0], "wbv", "--hub", "--robot-graph")
	explicit := fixture.robot(t, fixture.repositories[0], "wbv", "--hub", "--context", contexts[1], "--context", contexts[0], "--robot-graph")
	contextlessRead := fixture.robot(t, fixture.outside, "wbv", "--hub", "--contextless", "--robot-graph")
	allItems := fixture.robot(t, fixture.outside, "wbv", "--hub", "--robot-graph")

	wantHash := current["data_hash"]
	for name, output := range map[string]map[string]any{"explicit": explicit, "contextless": contextlessRead, "all": allItems} {
		if output["data_hash"] != wantHash {
			t.Errorf("%s data hash = %v, want canonical %v", name, output["data_hash"], wantHash)
		}
	}
	assertRobotIDs(t, current, []string{shared, visible})
	assertRobotIDs(t, explicit, []string{hidden, shared, visible})
	assertRobotIDs(t, contextlessRead, []string{contextless})
	assertRobotIDs(t, allItems, []string{contextless, hidden, shared, visible})
	if countRobotID(explicit, shared) != 1 {
		t.Fatalf("multi-context issue %s was not de-duplicated", shared)
	}
	if !robotHasBoundary(explicit, visible, hidden, "blocks") && !robotHasBoundary(current, visible, hidden, "blocks") {
		t.Fatalf("scoped graph lacks hidden blocker evidence for %s -> %s", visible, hidden)
	}

	plan := fixture.robot(t, fixture.repositories[0], "wbv", "--hub", "--robot-plan")
	if containsRobotID(plan, visible) {
		t.Fatalf("hidden blocker made %s ready", visible)
	}
}

func TestRealHubCorrelationAndLocalParity(t *testing.T) {
	fixture := newRealHubFixture(t)
	contexts := fixture.registerRepositories(t, 2)
	work := fixture.create(t, fixture.repositories[0], "Correlated work", "--type", "task")
	todo := fixture.create(t, fixture.repositories[0], "Uncorrelatable todo", "--type", "todo")

	fixture.runSuccess(t, fixture.repositories[0], "wbd", "link", work, "HEAD")
	records := fixture.ledger(t)
	if len(records) != 1 || records[0].BeadID != work || records[0].Context != contexts[0] {
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
	f.runSuccess(t, outside, "wbd", "bootstrap")
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
				if key != "boundary_refs" {
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

func robotHasBoundary(output map[string]any, issueID, endpointID, relationType string) bool {
	var walk func(any, string) bool
	walk = func(current any, owner string) bool {
		switch typed := current.(type) {
		case map[string]any:
			if id, ok := typed["id"].(string); ok {
				owner = id
			}
			if owner == issueID && typed["endpoint_id"] == endpointID && typed["relation_type"] == relationType {
				return true
			}
			for _, child := range typed {
				if walk(child, owner) {
					return true
				}
			}
		case []any:
			for _, child := range typed {
				if walk(child, owner) {
					return true
				}
			}
		}
		return false
	}
	return walk(output, "")
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
