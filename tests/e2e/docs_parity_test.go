package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/agents"
	"github.com/Dicklesworthstone/beads_viewer/pkg/drift"
	"github.com/Dicklesworthstone/beads_viewer/pkg/ui"
)

// repoFile reads a file relative to the repository root (tests/e2e/..).
func repoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// TestDocsParity_NoPendingMarkers: the 2026-09-01 reality check tagged every
// README sentence that described unshipped behaviour with a bv:pending
// marker. All of them were resolved; a new marker means a doc claim landed
// ahead of its code and must not ship.
func TestDocsParity_NoPendingMarkers(t *testing.T) {
	for _, rel := range []string{"README.md", "AGENTS.md", "docs/performance.md"} {
		if n := strings.Count(repoFile(t, rel), "bv:pending"); n != 0 {
			t.Errorf("%s still carries %d bv:pending marker(s); ship the code or remove the claim", rel, n)
		}
	}
}

// TestDocsParity_AlertTableMatchesCode: the README alert tables must name
// every alert type the drift package can emit, and every configuration key
// the tables mention must exist in the drift config.
func TestDocsParity_AlertTableMatchesCode(t *testing.T) {
	readme := repoFile(t, "README.md")
	start := strings.Index(readme, "## 🚨 Alerts System")
	if start < 0 {
		t.Fatalf("README has no Alerts System section")
	}
	section := readme[start:]
	if end := strings.Index(section, "### TUI Integration"); end > 0 {
		section = section[:end]
	}
	for _, typ := range drift.AllAlertTypes() {
		if !strings.Contains(section, "`"+string(typ)+"`") {
			t.Errorf("README alert tables do not document %q", typ)
		}
	}

	config := repoFile(t, "pkg/drift/config.go")
	keyRe := regexp.MustCompile("`([a-z_]+)` \\(")
	seen := map[string]bool{}
	for _, m := range keyRe.FindAllStringSubmatch(section, -1) {
		key := m[1]
		if seen[key] {
			continue
		}
		seen[key] = true
		if !strings.Contains(config, "yaml:\""+key+"\"") {
			t.Errorf("README documents drift key %q that pkg/drift/config.go does not define", key)
		}
	}
	if len(seen) < 10 {
		t.Errorf("expected the alert tables to document the .bv/drift.yaml keys, found %d", len(seen))
	}
}

// TestAgentsMD_HasRCHTrustBoundary: the RCH section must say what leaves the
// machine and what never may.
func TestAgentsMD_HasRCHTrustBoundary(t *testing.T) {
	agents := repoFile(t, "AGENTS-old.md")
	idx := strings.Index(agents, "### Trust boundary")
	if idx < 0 {
		t.Fatalf("AGENTS-old.md RCH section has no 'Trust boundary' subsection")
	}
	sub := agents[idx:]
	for _, want := range []string{"never be shipped", "fails open", "approval"} {
		if !strings.Contains(sub, want) {
			t.Errorf("RCH trust boundary subsection should mention %q", want)
		}
	}
}

// TestDocsParity_ReadmeBlurbMatchesGenerated: the "Ready-made Blurb" section
// of the README must be the same text bv installs into AGENTS.md
// (agents.AgentBlurb). Every non-blank line of the generated blurb (minus its
// HTML marker lines) has to appear verbatim in the README, so a change to one
// without the other fails here.
func TestDocsParity_ReadmeBlurbMatchesGenerated(t *testing.T) {
	readme := repoFile(t, "README.md")
	var missing []string
	for _, line := range strings.Split(agents.AgentBlurb, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "<!--") {
			continue
		}
		if !strings.Contains(readme, line) {
			missing = append(missing, line)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d generated blurb line(s) are not in README.md (regenerate the Ready-made Blurb section from pkg/agents/blurb.go):\n%s", len(missing), strings.Join(missing, "\n"))
	}
	if !strings.Contains(readme, agents.BlurbStartMarker) {
		t.Fatalf("README blurb section should carry the %s marker so agents can find the installed version", agents.BlurbStartMarker)
	}
}

// TestReadme_NoUnpinnedPipedInstallers (G1): the README must never tell
// users to pipe a script from the moving main branch into a shell; every
// raw.githubusercontent.com installer URL has to name a commit SHA.
func TestReadme_NoUnpinnedPipedInstallers(t *testing.T) {
	readme := repoFile(t, "README.md")
	if strings.Contains(readme, "beads_viewer/main/install") {
		t.Fatalf("README pipes an installer from the moving main branch; pin it to a commit SHA")
	}
	pinned := regexp.MustCompile(`raw\.githubusercontent\.com/Dicklesworthstone/beads_viewer/([0-9a-f]{40})/install\.(sh|ps1)`)
	if len(pinned.FindAllString(readme, -1)) < 2 {
		t.Fatalf("expected the install.sh and install.ps1 examples to be pinned to a 40-hex commit")
	}
}

// TestDocsParity_NoStaleBehaviourPhrases (F4): wording that once described
// behaviour the code does not have must not come back.
func TestDocsParity_NoStaleBehaviourPhrases(t *testing.T) {
	readme := repoFile(t, "README.md")
	for _, stale := range []string{
		"hooks are opt-in",    // hooks run whenever .bv/hooks.yaml exists; --no-hooks is the opt-out
		"relative timestamps", // markdown export writes absolute dates for comments
		"Windows requires Go 1.21",
		"dependency-aware scheduling", // forecast/capacity are heuristics, not a scheduler
	} {
		if strings.Contains(readme, stale) {
			t.Errorf("README still says %q", stale)
		}
	}
	for _, must := range []string{
		"BV_BACKGROUND_MODE", // startup default plus runtime promotion documented in the env table
		"### Cache",          // disk cache location, TTL, invalidation, opt-out
		"--no-hooks",         // the hooks opt-out
	} {
		if !strings.Contains(readme, must) {
			t.Errorf("README lost the %q documentation", must)
		}
	}
}

// bvEnvVarsInCode returns every BV_* environment variable name that appears
// as a string literal in non-test Go code, keyed by the first file naming it.
func bvEnvVarsInCode(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..")
	// Any BV_* string literal counts: most variables are read through named
	// constants (os.Getenv(EnvSemanticEmbedder)), so matching Getenv alone misses them.
	re := regexp.MustCompile(`"(BV_[A-Z0-9_]+)"`)
	found := map[string]string{}
	for _, dir := range []string{"cmd", "pkg", "internal"} {
		err := filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range re.FindAllStringSubmatch(string(data), -1) {
				if _, seen := found[m[1]]; !seen {
					found[m[1]] = strings.TrimPrefix(path, root+string(filepath.Separator))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	return found
}

// TestDocsParity_EnvVarsDocumented (F2): every BV_* variable the code reads
// has a row in the README environment table, and every documented row is
// read by the code. Variables that are internal wiring between bv and its
// own subprocesses are listed as exemptions with the reason.
func TestDocsParity_EnvVarsDocumented(t *testing.T) {
	readme := repoFile(t, "README.md")
	rowRe := regexp.MustCompile("(?m)^\\| `(BV_[A-Z0-9_]+)`")
	documented := map[string]bool{}
	for _, m := range rowRe.FindAllStringSubmatch(readme, -1) {
		documented[m[1]] = true
	}
	exempt := map[string]string{
		"BV_TEST_MODE":         "test harness switch, not a user setting",
		"BV_BROWSER_LOG":       "test harness capture of browser opens",
		"BV_SKIP_ENV_TESTS":    "test harness switch",
		"BV_HUB_CHANGE_SIGNAL": "internal Hub refresh signal transport",
		"BV_WBV_HUB_SCOPE":     "internal Hub scope transport",
	}
	inCode := bvEnvVarsInCode(t)
	if len(inCode) < 10 {
		t.Fatalf("scanner found only %d BV_* variables; the walk is broken", len(inCode))
	}
	for name, file := range inCode {
		if _, ok := exempt[name]; ok {
			continue
		}
		if !documented[name] {
			t.Errorf("%s is read in %s but has no row in the README environment table", name, file)
		}
	}
	for name := range documented {
		if _, ok := inCode[name]; !ok {
			t.Errorf("README documents %s but no non-test Go code reads it", name)
		}
	}
}

// TestDocsParity_RobotCommandsDocumented (F2): every robot command the
// binary advertises in --robot-capabilities must be mentioned in README.md
// and in the AGENTS.md blurb source, and every environment variable the
// capabilities payload lists must have a README row.
func TestDocsParity_RobotCommandsDocumented(t *testing.T) {
	bv := buildBvBinary(t)
	cmd := exec.Command(bv, "--robot-capabilities")
	cmd.Dir = t.TempDir()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("--robot-capabilities: %v", err)
	}
	var caps struct {
		Commands []struct {
			Name string `json:"name"`
			Flag string `json:"flag"`
		} `json:"commands"`
		EnvironmentVariables map[string]json.RawMessage `json:"environment_variables"`
	}
	if err := json.Unmarshal(out, &caps); err != nil {
		t.Fatalf("capabilities decode: %v", err)
	}
	if len(caps.Commands) < 20 {
		t.Fatalf("capabilities lists only %d commands; payload shape changed?", len(caps.Commands))
	}
	readme := repoFile(t, "README.md")
	for _, c := range caps.Commands {
		fields := strings.Fields(c.Flag) // "--robot-related ISSUE_ID": only the flag token must appear
		if len(fields) == 0 || strings.Contains(readme, fields[0]) {
			continue
		}
		t.Errorf("robot command %s is advertised by --robot-capabilities but README.md never mentions %s", c.Name, fields[0])
	}
	for name := range caps.EnvironmentVariables {
		if !strings.HasPrefix(name, "BV_") {
			continue
		}
		if !strings.Contains(readme, "| `"+name+"`") {
			t.Errorf("environment variable %s is advertised by --robot-capabilities but has no README env table row", name)
		}
	}
}

// TestDocsParity_KeyBindingsDocumented (F2): every key the TUI registers in
// its binding registry (the source of the shortcuts sidebar and help) must
// appear somewhere in the README's key tables, so a new binding cannot ship
// undocumented and a removed one cannot linger in the docs unnoticed.
func TestDocsParity_KeyBindingsDocumented(t *testing.T) {
	readme := repoFile(t, "README.md")
	// Keys are written in the README inside backticks, e.g. `Shift+Tab`,
	// `n` / `N`, `ctrl+d`; compare case-insensitively on the backticked form.
	lower := strings.ToLower(readme)
	var missing []string
	seen := map[string]bool{}
	for _, doc := range ui.GetKeyBindingDocs() {
		key := strings.TrimSpace(doc.Key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		documented := strings.Contains(lower, "`"+strings.ToLower(key)+"`")
		if !documented && key == "n/N" {
			documented = strings.Contains(lower, "`n` / `n`")
		}
		if !documented {
			missing = append(missing, key+" ("+doc.Desc+", "+doc.Category+")")
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d registered key binding(s) are not documented in README.md:\n%s", len(missing), strings.Join(missing, "\n"))
	}
}
