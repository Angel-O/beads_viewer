package ui

import "testing"

import (
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
)

func TestRuntimeIssuePathsFallbackToSelectedSource(t *testing.T) {
	selected, changed := runtimeIssuePaths("issues.jsonl", RuntimeServices{})
	if selected != "issues.jsonl" || changed != "issues.jsonl" {
		t.Fatalf("runtimeIssuePaths() = %q, %q", selected, changed)
	}

	selected, changed = runtimeIssuePaths("issues.jsonl", RuntimeServices{
		SelectedIssuePath: "selected.jsonl",
		IssueChangePath:   "changes.jsonl",
	})
	if selected != "selected.jsonl" || changed != "changes.jsonl" {
		t.Fatalf("runtimeIssuePaths(explicit) = %q, %q", selected, changed)
	}
}

func TestWorkerConfigForRuntimePreservesResolvedServices(t *testing.T) {
	source := &testChangeSource{changes: make(chan struct{}, 1)}
	config := workerConfigForRuntime("issues.jsonl", RuntimeServices{
		SelectedIssuePath:  "selected.jsonl",
		IssueChangePath:    "changes.jsonl",
		CatalogPath:        "hub.yaml",
		SourceChangeSource: source,
	})
	if config.BeadsPath != "issues.jsonl" || config.SelectedIssuePath != "selected.jsonl" || config.IssueChangePath != "changes.jsonl" {
		t.Fatalf("worker paths = %#v", config)
	}
	if config.CatalogPath != "hub.yaml" || config.SourceChangeSource != source {
		t.Fatalf("worker resolved services = %#v", config)
	}
}

func TestRuntimeServicesUseExplicitPresentationState(t *testing.T) {
	resolver := func(model.Issue) []string { return []string{"ctx:alpha"} }
	loader := func(string, []model.Issue) (repositorypkg.Catalog, error) {
		return repositorypkg.Catalog{{ID: "ctx:alpha", Kind: repositorypkg.IdentityExact}}, nil
	}
	m := NewModel([]model.Issue{{ID: "alpha", Labels: []string{"ctx:alpha"}}}, nil, "", RuntimeServices{
		CatalogPath:             "resolved-catalog",
		CatalogLoader:           loader,
		IssueRepositoryResolver: resolver,
	})
	defer m.Stop()
	if m.hubRepositoryMode || m.usesHubScope() {
		t.Fatal("catalog/resolver capabilities inferred repository presentation without explicit state")
	}

	m.SetRuntimeServices(RuntimeServices{
		CatalogPath:             "resolved-catalog",
		CatalogLoader:           loader,
		IssueRepositoryResolver: resolver,
		RepositoryPresentation:  true,
	})
	if !m.hubRepositoryMode || !m.usesHubScope() {
		t.Fatal("explicit repository presentation state was not consumed")
	}
}
