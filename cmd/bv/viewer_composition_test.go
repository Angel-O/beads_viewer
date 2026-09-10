package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/ui"
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
		name, mode, config, wantMode       string
		wantStore, wantRepository, hubMode bool
	}{
		{name: "git", mode: "git", wantMode: "git"},
		{name: "external", mode: "external", config: config, wantMode: "external", wantStore: true},
		{name: "hub-external", mode: "external", config: config, wantMode: "external", wantStore: true, wantRepository: true, hubMode: true},
		{name: "hub-history-off", mode: "off", config: config, wantMode: "off", wantStore: true, wantRepository: true, hubMode: true},
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
				HubMode:        test.hubMode,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got.HistoryProvider.Mode() != test.wantMode || got.UsesHubConfigStore != test.wantStore {
				t.Fatalf("mode/store = %s/%v, want %s/%v", got.HistoryProvider.Mode(), got.UsesHubConfigStore, test.wantMode, test.wantStore)
			}
			if test.wantRepository != got.RepositoryPresentation {
				t.Fatalf("repository presentation = %v, want %v", got.RepositoryPresentation, test.wantRepository)
			}
			if (got.LabelPredicate != nil) != test.wantRepository {
				t.Fatalf("label admission supplied = %v, want %v", got.LabelPredicate != nil, test.wantRepository)
			}
			if (got.CatalogLoader != nil) != test.wantRepository || (got.IssueRepositoryResolver != nil) != test.wantRepository {
				t.Fatalf("Hub-only repository services supplied = loader:%v resolver:%v, want %v", got.CatalogLoader != nil, got.IssueRepositoryResolver != nil, test.wantRepository)
			}
			if test.name == "off" {
				if got.SemanticStorePath == "" || got.HubConfigPath != config || len(got.MetadataChangePaths) != 1 {
					t.Fatalf("off composition lost non-history services: %#v", got)
				}
				if got.InitialRepositorySelection != nil {
					t.Fatalf("off composition resolved a repository selection: %#v", got.InitialRepositorySelection)
				}
				if _, err := got.HistoryProvider.GenerateReport(context.Background(), nil, correlation.CorrelatorOptions{}); err != nil {
					t.Fatalf("off provider invoked history source: %v", err)
				}
			}
			if test.hubMode {
				wantIndexDir := filepath.Join(filepath.Dir(got.SemanticStorePath), "semantic")
				if got.SemanticIndexDir != wantIndexDir {
					t.Fatalf("semantic index directory = %q, want %q", got.SemanticIndexDir, wantIndexDir)
				}
			} else if got.SemanticIndexDir != "" {
				t.Fatalf("local semantic index directory = %q, want empty", got.SemanticIndexDir)
			}
		})
	}
	local, err := composeViewerServices(viewerCompositionInput{HistoryMode: "git", HubConfigPath: config, WorkDir: root})
	if err != nil {
		t.Fatal(err)
	}
	localServices := local.runtimeServicesFor("", nil)
	if localServices.CatalogPath != "" || localServices.CatalogLoader != nil || localServices.IssueRepositoryResolver != nil || localServices.SemanticIndexDir != "" || localServices.RepositoryPresentation || localServices.AutoRefresh || localServices.SourceChangeSource != nil || localServices.CatalogChangeSource != nil {
		t.Fatalf("local composition exposed Hub repository services: %#v", localServices)
	}
}

func TestLocalCompositionDoesNotGainHubCapabilitiesFromHistoryConfig(t *testing.T) {
	root := t.TempDir()
	config := writeCompositionHubConfig(t, root)

	composition, err := composeViewerServices(viewerCompositionInput{
		HistoryMode:   "external",
		HubConfigPath: config,
		WorkDir:       root,
		RobotMode:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !composition.UsesHubConfigStore || composition.HistoryProvider.Mode() != "external" {
		t.Fatalf("history composition = store:%v mode:%q, want external history from config", composition.UsesHubConfigStore, composition.HistoryProvider.Mode())
	}
	if composition.CatalogLoader != nil || composition.IssueRepositoryResolver != nil || composition.LabelPredicate != nil || composition.SemanticIndexDir != "" || composition.InitialRepositorySelection != nil || composition.CurrentRepositoryID != "" || composition.RepositoryPresentation || composition.AutoRefresh || composition.SourceChangeSource != nil || composition.CatalogChangeSource != nil || composition.HubScopeSnapshot != nil || composition.HubScopeMemberIDs != nil || composition.ScopeServices.Load != nil {
		t.Fatalf("local composition exposed Hub capabilities: %#v", composition)
	}
}

func TestViewerCompositionBuildsNeutralRuntimeServices(t *testing.T) {
	root := t.TempDir()
	config := writeCompositionHubConfig(t, root)
	issuePath := filepath.Join(root, "issues.jsonl")
	if err := os.WriteFile(issuePath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	composition, err := composeViewerServices(viewerCompositionInput{
		HistoryMode:    "external",
		HubConfigPath:  config,
		ExplicitDBPath: issuePath,
		WorkspacePath:  "",
		WorkDir:        root,
		HubMode:        true,
		RobotMode:      true,
	})
	if err != nil {
		t.Fatal(err)
	}

	initialScope := &ui.ScopeSnapshot{}
	composition.CurrentRepositoryID = "ctx:alpha"
	services := composition.runtimeServicesFor("", initialScope)
	if services.HistoryProvider != composition.HistoryProvider || services.SelectedIssuePath != composition.SelectedIssuePath || services.IssueChangePath != composition.IssueChangePath {
		t.Fatalf("runtime source/history = %#v, want composition values", services)
	}
	if services.SemanticDatasetPath != composition.SemanticDatasetPath || services.SemanticStorePath != composition.SemanticStorePath || services.SemanticIndexDir != composition.SemanticIndexDir {
		t.Fatalf("runtime search paths = %#v, want composition values", services)
	}
	if services.CatalogPath != config || services.CatalogLoader == nil || services.IssueRepositoryResolver == nil || services.LabelPredicate == nil {
		t.Fatalf("runtime Hub services = %#v", services)
	}
	if got := services.IssueRepositoryResolver(model.Issue{Labels: []string{"ctx:alpha", "work"}}); len(got) != 1 || got[0] != "ctx:alpha" {
		t.Fatalf("resolved issue repositories = %v, want [ctx:alpha]", got)
	}
	if services.CurrentRepositoryID != "ctx:alpha" {
		t.Fatalf("runtime current repository ID = %q, want ctx:alpha", services.CurrentRepositoryID)
	}
	if !services.ExternalHistory || !services.RepositoryPresentation || !services.AutoRefresh || services.InitialScope != initialScope {
		t.Fatalf("runtime policy = %#v", services)
	}
	if services.SourceChangeSource == nil || services.CatalogChangeSource == nil {
		t.Fatalf("runtime refresh sources were not composed: %#v", services)
	}
}

func TestViewerCompositionResolvesRefreshOptOutBeforeUI(t *testing.T) {
	root := t.TempDir()
	config := writeCompositionHubConfig(t, root)
	writeFakeWBD(t, `{"id":"scope-a"}`, filepath.Join(root, "wbd-calls"))
	composition, err := composeViewerServices(viewerCompositionInput{
		HistoryMode:        "external",
		HubConfigPath:      config,
		WorkDir:            root,
		HubMode:            true,
		RefreshEnvironment: "0",
		RobotMode:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	services := composition.runtimeServicesFor("", nil)
	if !services.RepositoryPresentation || services.AutoRefresh || services.SourceChangeSource != nil || services.CatalogChangeSource != nil {
		t.Fatalf("refresh opt-out was not resolved at composition: %#v", services)
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

func TestComposeViewerServicesHistoryOffPreservesExplicitDB(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
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

	got, err := composeViewerServices(viewerCompositionInput{
		HistoryMode:    "off",
		ExplicitDBPath: explicitDir,
		WorkDir:        root,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(explicitDir, "issues.jsonl")
	if got.SelectedIssuePath != want {
		t.Fatalf("history-off selected issue path = %q, want explicit source %q", got.SelectedIssuePath, want)
	}
}

func TestComposeViewerServicesExplicitDBOverridesHubStore(t *testing.T) {
	root := t.TempDir()
	config := writeCompositionHubConfig(t, root)
	explicitDir := filepath.Join(root, "explicit")
	if err := os.MkdirAll(explicitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(explicitDir, "issues.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := composeViewerServices(viewerCompositionInput{
		HistoryMode:    "external",
		HubConfigPath:  config,
		ExplicitDBPath: explicitDir,
		WorkDir:        root,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(explicitDir, "issues.jsonl")
	if got.SelectedIssuePath != want {
		t.Fatalf("Hub-config selected issue path = %q, want explicit source %q", got.SelectedIssuePath, want)
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
	if got.HubScopeSnapshot == nil || got.HubScopeMemberIDs == nil || got.SourceChangeSource == nil || got.CatalogChangeSource == nil {
		t.Fatalf("Hub scope composition = snapshot %v, members %v, source %v, catalog %v", got.HubScopeSnapshot != nil, got.HubScopeMemberIDs != nil, got.SourceChangeSource != nil, got.CatalogChangeSource != nil)
	}
	if got.ScopeServices.QueryBacklog == nil || got.ScopeServices.LoadDetails == nil || got.ScopeServices.Mutate == nil || got.ScopeServices.MutateMatching == nil {
		t.Fatalf("typed Hub scope services were not composed: %#v", got.ScopeServices)
	}

	local, err := composeViewerServices(viewerCompositionInput{HistoryMode: "git", WorkDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if local.HubScopeSnapshot != nil || local.HubScopeMemberIDs != nil || local.SourceChangeSource != nil || local.CatalogChangeSource != nil {
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
