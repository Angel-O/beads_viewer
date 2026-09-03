package correlation

import (
	"context"
	"reflect"
	"testing"
)

func TestProviderConstructors(t *testing.T) {
	git := NewGitProvider("repo", "issues.jsonl")
	if git.mode != HistoryModeGit || git.repositoryRoot != "repo" || git.issuePath != "issues.jsonl" {
		t.Fatalf("git provider = %#v", git)
	}
	if external := NewExternalProvider("hub.yaml"); external.mode != HistoryModeExternal || external.configPath != "hub.yaml" {
		t.Fatalf("external provider = %#v", external)
	}
	if disabled := NewDisabledProvider(); disabled.mode != HistoryModeOff {
		t.Fatalf("disabled provider = %#v", disabled)
	}
}

func TestProviderWithFeedbackStoreCopiesSourceSelection(t *testing.T) {
	store := NewFeedbackStore(t.TempDir())
	original := NewGitProvider("repo", "issues.jsonl")
	decorated := original.WithFeedbackStore(store)

	if decorated == original {
		t.Fatal("feedback-aware provider should be a copy")
	}
	if original.feedback != nil {
		t.Fatal("source provider was mutated")
	}
	if decorated.feedback != store {
		t.Fatal("feedback store was not attached to provider copy")
	}
	if got := decorated.correlator(context.Background()); got.feedback != store {
		t.Fatal("provider correlator did not attach feedback store")
	}
}

func TestDisabledProviderMatchesCorrelatorOffReport(t *testing.T) {
	beads := []BeadInfo{{ID: "bv-1", Title: "one", IssueType: "task", Status: "open"}}
	got, err := NewDisabledProvider().GenerateReport(context.Background(), beads, CorrelatorOptions{})
	if err != nil {
		t.Fatalf("provider report: %v", err)
	}
	want := &HistoryReport{
		GeneratedAt: got.GeneratedAt,
		DataHash:    got.DataHash,
		GitRange:    "history disabled",
		Stats:       got.Stats,
		Histories:   got.Histories,
		CommitIndex: got.CommitIndex,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("disabled report has unexpected metadata:\ngot:  %#v\nwant: %#v", got, want)
	}
}
