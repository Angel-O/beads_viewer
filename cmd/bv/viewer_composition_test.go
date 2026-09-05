package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
)

func writeCompositionHubConfig(t *testing.T, root string) string {
	t.Helper()
	store := filepath.Join(root, "hub-store", ".beads")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "hub.yaml")
	contents := "version: 1\nstore: " + store + "\nledger: " + filepath.Join(root, "ledger.jsonl") + "\nrepositories: {}\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestComposeViewerServicesSelectsHistoryProviders(t *testing.T) {
	root := t.TempDir()
	config := writeCompositionHubConfig(t, root)
	issuePath := filepath.Join(root, "issues.jsonl")
	if err := os.WriteFile(issuePath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	writeFakeWBD(t, `{"id":"scope-a"}`, filepath.Join(root, "wbd-calls"))

	tests := []struct {
		name, mode, config, wantMode string
		wantStore                    bool
	}{
		{name: "git", mode: "git", wantMode: "git"},
		{name: "external", mode: "external", config: config, wantMode: "external", wantStore: true},
		{name: "off", mode: "off", config: config, wantMode: "off", wantStore: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := composeViewerServices(viewerCompositionInput{
				HistoryMode:    test.mode,
				HubConfigPath:  test.config,
				ExplicitDBPath: issuePath,
				WorkDir:        root,
				RobotMode:      true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got.HistoryProvider.Mode() != test.wantMode || got.UsesHubConfigStore != test.wantStore {
				t.Fatalf("mode/store = %s/%v, want %s/%v", got.HistoryProvider.Mode(), got.UsesHubConfigStore, test.wantMode, test.wantStore)
			}
			if test.wantStore != got.RepositoryPresentation {
				t.Fatalf("repository presentation = %v, want %v", got.RepositoryPresentation, test.wantStore)
			}
			if test.name == "off" {
				if got.SemanticStorePath == "" || got.HubConfigPath != config || len(got.MetadataChangePaths) != 1 {
					t.Fatalf("off composition lost non-history services: %#v", got)
				}
				if got.DefaultCurrentContext != "" {
					t.Fatalf("off composition resolved Git-backed current context: %q", got.DefaultCurrentContext)
				}
				if _, err := got.HistoryProvider.GenerateReport(context.Background(), nil, correlation.CorrelatorOptions{}); err != nil {
					t.Fatalf("off provider invoked history source: %v", err)
				}
			}
		})
	}
}

func TestDecodeHubRobotFilterPreservesContextSelection(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "hub.yaml")
	if err := os.WriteFile(config, []byte("version: 1\nstore: "+filepath.Join(root, "store")+"\nledger: "+filepath.Join(root, "ledger.jsonl")+"\nrepositories:\n  ctx:alpha:\n    path: "+root+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	selection, err := decodeHubRobotFilter(`{"mode":"contexts","contexts":["ctx:alpha"],"include_contextless":true}`, config)
	if err != nil {
		t.Fatal(err)
	}
	if selection == nil || selection.Mode != "contexts" || len(selection.Contexts) != 1 || !selection.IncludeContextless {
		t.Fatalf("decoded wbv selection = %#v", selection)
	}
	if _, err := decodeHubRobotFilter(`{"mode":"contexts","contexts":["ctx:alpha"],"extra":true}`, config); err == nil {
		t.Fatal("unknown wbv selection field was accepted")
	}
}

func TestComposeViewerServicesAutoDefaultsToGitWithoutHubConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	got, err := composeViewerServices(viewerCompositionInput{HistoryMode: "auto", WorkDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if got.HistoryProvider.Mode() != "git" || got.UsesHubConfigStore {
		t.Fatalf("auto composition = mode %q/store %v, want git/false", got.HistoryProvider.Mode(), got.UsesHubConfigStore)
	}
}

func TestComposeViewerServicesPreservesExplicitDBPrecedence(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "env")
	explicitDir := filepath.Join(root, "explicit")
	for _, dir := range []string{envDir, explicitDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "issues.jsonl"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("BEADS_DB", envDir)
	got, err := composeViewerServices(viewerCompositionInput{HistoryMode: "git", ExplicitDBPath: explicitDir, WorkDir: root})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(explicitDir, "issues.jsonl")
	if got.SelectedIssuePath != want {
		t.Fatalf("selected issue path = %q, want explicit source %q", got.SelectedIssuePath, want)
	}
}

func TestComposeViewerServicesRejectsHubStoreWorkspaceAndAsOf(t *testing.T) {
	root := t.TempDir()
	config := writeCompositionHubConfig(t, root)
	for name, input := range map[string]viewerCompositionInput{
		"workspace": {HistoryMode: "external", HubConfigPath: config, WorkspacePath: filepath.Join(root, "workspace.yaml")},
		"as-of":     {HistoryMode: "external", HubConfigPath: config, AsOf: "HEAD~1"},
	} {
		t.Run(name, func(t *testing.T) {
			input.WorkDir = root
			if _, err := composeViewerServices(input); err == nil {
				t.Fatal("expected configured Hub store restriction")
			}
		})
	}
}

func TestComposeViewerServicesProvidesBoundedHubScopeSeam(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	store := filepath.Join(root, ".local", "share", "beads", "hub", ".beads")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "hub.yaml")
	if err := os.WriteFile(config, []byte("version: 1\nstore: "+store+"\nledger: "+filepath.Join(root, "ledger.jsonl")+"\nrepositories: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeFakeWBD(t, `{"id":"scope-a"}`, filepath.Join(root, "wbd-calls"))
	got, err := composeViewerServices(viewerCompositionInput{
		HistoryMode:   "external",
		HubConfigPath: config,
		WorkDir:       root,
		HubMode:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.HubScopeSnapshot == nil || got.HubScopeMemberIDs == nil || got.HubChangeSignal == "" {
		t.Fatalf("Hub scope composition = snapshot %v, members %v, signal %q", got.HubScopeSnapshot != nil, got.HubScopeMemberIDs != nil, got.HubChangeSignal)
	}

	local, err := composeViewerServices(viewerCompositionInput{HistoryMode: "git", WorkDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if local.HubScopeSnapshot != nil || local.HubScopeMemberIDs != nil || local.HubChangeSignal != "" {
		t.Fatalf("local composition acquired Hub scope loading: %#v", local)
	}
}

func TestComposeExternalHistoryDoesNotNeedHubScopeLoader(t *testing.T) {
	root := t.TempDir()
	config := writeCompositionHubConfig(t, root)
	t.Setenv("PATH", filepath.Join(root, "missing-bin"))
	got, err := composeViewerServices(viewerCompositionInput{
		HistoryMode:   "external",
		HubConfigPath: config,
		WorkDir:       root,
		RobotMode:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.HubScopeSnapshot != nil || got.HubScopeMemberIDs != nil {
		t.Fatalf("non-Hub external composition acquired scope loading: %#v", got)
	}
}

func TestComposeViewerServicesRejectsWBDStoreMismatch(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	config := writeCompositionHubConfig(t, root)
	_, err := composeViewerServices(viewerCompositionInput{
		HistoryMode:   "external",
		HubConfigPath: config,
		WorkDir:       root,
		HubMode:       true,
	})
	if err == nil || !strings.Contains(err.Error(), "does not match wbd default Hub store") {
		t.Fatalf("store mismatch error = %v", err)
	}
}

func TestComposeViewerServicesReportsMissingWBD(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	store := filepath.Join(root, ".local", "share", "beads", "hub", ".beads")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "hub.yaml")
	if err := os.WriteFile(config, []byte("version: 1\nstore: "+store+"\nledger: "+filepath.Join(root, "ledger.jsonl")+"\nrepositories: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Join(root, "missing-bin"))
	_, err := composeViewerServices(viewerCompositionInput{
		HistoryMode:   "external",
		HubConfigPath: config,
		WorkDir:       root,
		HubMode:       true,
	})
	if err == nil || !strings.Contains(err.Error(), "requires wbd") {
		t.Fatalf("missing wbd error = %v", err)
	}
}
