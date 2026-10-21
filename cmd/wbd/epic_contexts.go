package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
)

type epicReconcileOutput struct {
	EpicsProcessed  int `json:"epics_processed"`
	EpicsReconciled int `json:"epics_reconciled"`
	EpicsSkipped    int `json:"epics_skipped"`
}

// reconcileEpicContexts is the one-shot transition path for legacy materialized
// epic labels. Each update is atomic in the backend and reruns skip matching
// records.
func (a *app) reconcileEpicContexts() int {
	data, err := a.runBDCapture(a.dir, "--json", "list", "--no-directory-labels", "--all", "--include-all-types", "--limit", "0")
	if err != nil {
		return a.fail(fmt.Errorf("reading authoritative issues: %w", err))
	}
	var summaries []bdIssue
	if err := json.Unmarshal(data, &summaries); err != nil {
		return a.fail(fmt.Errorf("decoding authoritative issue list: %w", err))
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].ID < summaries[j].ID })

	issues := make(map[string]bdIssue, len(summaries))
	for _, summary := range summaries {
		if summary.ID == "" {
			return a.fail(errors.New("authoritative issue list contains an empty issue ID"))
		}
		issue, err := a.readIssue(summary.ID, false)
		if err != nil {
			return a.fail(err)
		}
		issues[issue.ID] = issue
	}

	epics := make([]bdIssue, 0)
	for _, issue := range issues {
		if issue.IssueType == "epic" {
			epics = append(epics, issue)
		}
	}
	sort.Slice(epics, func(i, j int) bool { return epics[i].ID < epics[j].ID })
	result := epicReconcileOutput{EpicsProcessed: len(epics)}
	for _, epic := range epics {
		derived := derivedEpicContexts(epic.ID, issues)
		if sameStrings(hub.Contexts(epic.Labels), derived) {
			result.EpicsSkipped++
			continue
		}

		labels := ordinaryLabels(epic.Labels)
		labels = append(labels, derived...)
		args := []string{"--json", "update", epic.ID}
		if len(labels) == 0 {
			args = append(args, "--set-labels", "")
		} else {
			for _, label := range labels {
				args = append(args, "--set-labels", label)
			}
		}
		if _, err := a.runBDCapture(a.dir, args...); err != nil {
			return a.fail(fmt.Errorf("reconciling epic %s: %w", epic.ID, err))
		}
		a.signalMutation("epic context reconciliation")
		result.EpicsReconciled++
	}
	return a.writeJSON(result)
}

func derivedEpicContexts(epicID string, issues map[string]bdIssue) []string {
	seen := make(map[string]struct{})
	for _, child := range issues {
		for _, relation := range child.Dependencies {
			if relation.ID != epicID || relation.DependencyType != "parent-child" {
				continue
			}
			for _, context := range hub.Contexts(child.Labels) {
				seen[context] = struct{}{}
			}
		}
	}
	contexts := make([]string, 0, len(seen))
	for context := range seen {
		contexts = append(contexts, context)
	}
	sort.Strings(contexts)
	return contexts
}

func ordinaryLabels(labels []string) []string {
	ordinary := make([]string, 0, len(labels))
	for _, label := range labels {
		if !hub.IsContextLabel(label) {
			ordinary = append(ordinary, label)
		}
	}
	return ordinary
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func epicContextMismatch(epic bdIssue, issues map[string]bdIssue) *compatibilityFinding {
	stored := hub.Contexts(epic.Labels)
	derived := derivedEpicContexts(epic.ID, issues)
	if sameStrings(stored, derived) {
		return nil
	}
	return &compatibilityFinding{
		Code:    "epic_context_mismatch",
		IssueID: epic.ID,
		Value:   strings.Join(stored, ",") + " != " + strings.Join(derived, ","),
		Message: "stored epic contexts differ from the direct-child context union",
	}
}
