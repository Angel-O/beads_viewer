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
