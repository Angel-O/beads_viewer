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

func TestWorkspaceRobotTriageCleanOutput(t *testing.T) {
	bv := buildBvBinary(t)

	workspaceRoot := t.TempDir()
	configPath := filepath.Join(workspaceRoot, ".bv", "workspace.yaml")

	// Create two repos with issues.
	apiBeadsDir := filepath.Join(workspaceRoot, "services", "api", ".beads")
	webBeadsDir := filepath.Join(workspaceRoot, "apps", "web", ".beads")
	if err := os.MkdirAll(apiBeadsDir, 0o755); err != nil {
		t.Fatalf("mkdir api beads: %v", err)
	}
	if err := os.MkdirAll(webBeadsDir, 0o755); err != nil {
		t.Fatalf("mkdir web beads: %v", err)
	}

	apiIssues := `{"id":"AUTH-1","title":"API auth","status":"open","priority":1,"issue_type":"task"}`
	if err := os.WriteFile(filepath.Join(apiBeadsDir, "issues.jsonl"), []byte(apiIssues+"\n"), 0o644); err != nil {
		t.Fatalf("write api issues.jsonl: %v", err)
	}

	// Cross-repo dependency references must already be namespaced.
	webIssues := `{"id":"UI-1","title":"Web UI","status":"open","priority":2,"issue_type":"task","dependencies":[{"issue_id":"UI-1","depends_on_id":"api-AUTH-1","type":"blocks"}]}`
	if err := os.WriteFile(filepath.Join(webBeadsDir, "issues.jsonl"), []byte(webIssues+"\n"), 0o644); err != nil {
		t.Fatalf("write web issues.jsonl: %v", err)
	}

	config := `
name: test-workspace
repos:
  - name: api
    path: services/api
    prefix: api-
  - name: web
    path: apps/web
    prefix: web-
discovery:
  enabled: false
`
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir .bv: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write workspace.yaml: %v", err)
	}

	cmd := exec.Command(bv, "--robot-triage", "--workspace", configPath)
	cmd.Dir = workspaceRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("--robot-triage --workspace failed: %v\nstderr=%s\nstdout=%s", err, stderr.String(), stdout.String())
	}
	if got := strings.TrimSpace(stderr.String()); got != "" {
		t.Fatalf("expected empty stderr for robot JSON, got: %s", got)
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON on stdout: %v\nstdout=%s", err, stdout.String())
	}
	if _, ok := payload["generated_at"]; !ok {
		t.Fatalf("missing generated_at")
	}
	if _, ok := payload["triage"]; !ok {
		t.Fatalf("missing triage")
	}
}

// writeWorkspaceFixture builds a two-repo workspace: api (AUTH-1, AUTH-2)
// and web (UI-1 blocked by api-AUTH-1), with discovery disabled so only the
// listed repos load. It returns the workspace root.
func writeWorkspaceFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("services/api/.beads/issues.jsonl",
		`{"id":"AUTH-1","title":"API auth","status":"open","priority":1,"issue_type":"task"}
{"id":"AUTH-2","title":"API tokens","status":"open","priority":2,"issue_type":"task","dependencies":[{"issue_id":"AUTH-2","depends_on_id":"AUTH-1","type":"blocks"}]}
`)
	write("apps/web/.beads/issues.jsonl",
		`{"id":"UI-1","title":"Web UI","status":"open","priority":2,"issue_type":"task","dependencies":[{"issue_id":"UI-1","depends_on_id":"api-AUTH-1","type":"blocks"}]}
`)
	write(".bv/workspace.yaml", `
name: test-workspace
repos:
  - name: api
    path: services/api
    prefix: api-
  - name: web
    path: apps/web
    prefix: web-
discovery:
  enabled: false
`)
	// A directory with no .beads of its own, nested under the workspace root.
	if err := os.MkdirAll(filepath.Join(root, "docs", "notes"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	return root
}

type workspaceGraphPayload struct {
	SourceKind string `json:"source_kind"`
	SourcePath string `json:"source_path"`
	Scope      struct {
		Repo string `json:"repo"`
	} `json:"scope"`
	Adjacency struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
		Edges []struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"edges"`
	} `json:"adjacency"`
}

func runWorkspaceGraph(t *testing.T, bv, dir string, extra ...string) workspaceGraphPayload {
	t.Helper()
	cmd := exec.Command(bv, append([]string{"--robot-graph"}, extra...)...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("--robot-graph %v in %s failed: %v\nstderr=%s\nstdout=%s", extra, dir, err, stderr.String(), stdout.String())
	}
	if got := strings.TrimSpace(stderr.String()); got != "" {
		t.Fatalf("robot JSON must keep stderr empty, got: %s", got)
	}
	var p workspaceGraphPayload
	if err := json.Unmarshal(stdout.Bytes(), &p); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout=%s", err, stdout.String())
	}
	return p
}

// TestWorkspaceAutoDiscoveryFromNestedDir (I2): with no .beads reachable, bv
// finds .bv/workspace.yaml in a parent directory by itself, namespaces every
// issue with its repo prefix, keeps the cross-repo dependency, and --repo
// narrows the graph to one repo.
func TestWorkspaceAutoDiscoveryFromNestedDir(t *testing.T) {
	bv := buildBvBinary(t)
	root := writeWorkspaceFixture(t)
	nested := filepath.Join(root, "docs", "notes")

	p := runWorkspaceGraph(t, bv, nested)
	if p.SourceKind != "workspace" || p.SourcePath != filepath.Join(root, ".bv", "workspace.yaml") {
		t.Fatalf("envelope source=%s/%s; want workspace/%s", p.SourceKind, p.SourcePath, filepath.Join(root, ".bv", "workspace.yaml"))
	}
	ids := map[string]bool{}
	for _, n := range p.Adjacency.Nodes {
		ids[n.ID] = true
	}
	for _, want := range []string{"api-AUTH-1", "api-AUTH-2", "web-UI-1"} {
		if !ids[want] {
			t.Fatalf("namespaced id %s missing from nodes %v", want, ids)
		}
	}
	if ids["AUTH-1"] || ids["UI-1"] {
		t.Fatalf("unprefixed ids leaked into the workspace graph: %v", ids)
	}
	var crossRepo, intraRepo bool
	for _, e := range p.Adjacency.Edges {
		if (e.From == "web-UI-1" && e.To == "api-AUTH-1") || (e.From == "api-AUTH-1" && e.To == "web-UI-1") {
			crossRepo = true
		}
		if (e.From == "api-AUTH-2" && e.To == "api-AUTH-1") || (e.From == "api-AUTH-1" && e.To == "api-AUTH-2") {
			intraRepo = true
		}
	}
	if !crossRepo || !intraRepo {
		t.Fatalf("expected the cross-repo (web→api) and intra-repo edges, got %+v", p.Adjacency.Edges)
	}

	// --repo api: only the api repo's two issues and their edge remain.
	api := runWorkspaceGraph(t, bv, nested, "--repo", "api")
	if len(api.Adjacency.Nodes) != 2 || api.Scope.Repo != "api" {
		t.Fatalf("--repo api: nodes=%d scope.repo=%q; want 2 nodes scoped to api: %+v", len(api.Adjacency.Nodes), api.Scope.Repo, api.Adjacency.Nodes)
	}
	for _, n := range api.Adjacency.Nodes {
		if !strings.HasPrefix(n.ID, "api-") {
			t.Fatalf("--repo api leaked %s", n.ID)
		}
	}

	// From the workspace root itself (no .beads there either) discovery also applies.
	if rootView := runWorkspaceGraph(t, bv, root); len(rootView.Adjacency.Nodes) != 3 {
		t.Fatalf("from the workspace root: nodes=%d; want 3", len(rootView.Adjacency.Nodes))
	}

	// Inside a member repo the repo's own .beads wins unless --workspace is passed.
	single := runWorkspaceGraph(t, bv, filepath.Join(root, "services", "api"))
	if single.SourceKind == "workspace" || len(single.Adjacency.Nodes) != 2 {
		t.Fatalf("inside services/api expected the single-repo view (2 nodes), got source=%s nodes=%d", single.SourceKind, len(single.Adjacency.Nodes))
	}
	forced := runWorkspaceGraph(t, bv, filepath.Join(root, "services", "api"), "--workspace", filepath.Join(root, ".bv", "workspace.yaml"))
	if forced.SourceKind != "workspace" || len(forced.Adjacency.Nodes) != 3 {
		t.Fatalf("--workspace override inside a member repo: source=%s nodes=%d; want workspace/3", forced.SourceKind, len(forced.Adjacency.Nodes))
	}
}
