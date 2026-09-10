package ui

import "testing"

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
	config := workerConfigForRuntime("issues.jsonl", RuntimeServices{
		SelectedIssuePath: "selected.jsonl",
		IssueChangePath:   "changes.jsonl",
		CatalogPath:       "hub.yaml",
		HubChangeSignal:   "signal",
	})
	if config.BeadsPath != "issues.jsonl" || config.SelectedIssuePath != "selected.jsonl" || config.IssueChangePath != "changes.jsonl" {
		t.Fatalf("worker paths = %#v", config)
	}
	if config.CatalogPath != "hub.yaml" || config.HubChangeSignal != "signal" {
		t.Fatalf("worker resolved services = %#v", config)
	}
}
