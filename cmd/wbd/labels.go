package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
)

type bulkLabelResult struct {
	ID     string    `json:"id"`
	Status string    `json:"status"`
	Labels *[]string `json:"labels,omitempty"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	OutcomeUnknown bool `json:"outcome_unknown,omitempty"`
}

type bulkLabelResponse struct {
	Operation string            `json:"operation"`
	Matched   int               `json:"matched"`
	Applied   int               `json:"applied"`
	Errors    int               `json:"errors"`
	Results   []bulkLabelResult `json:"results"`
}

func (a *app) labels(request request) int {
	config, err := hub.Resolve(a.paths.Config)
	if err != nil {
		return a.fail(err)
	}
	targets := append([]string(nil), request.positionals...)
	if request.labelsEpic != "" {
		root, readErr := a.readIssue(request.labelsEpic, false)
		if readErr != nil {
			return a.fail(readErr)
		}
		if root.IssueType != "epic" {
			return a.fail(&hub.PolicyError{Code: hub.PolicyInvalidKind, Field: "type", Value: root.IssueType, Message: "--epic requires an epic issue"})
		}
		if err := hub.ValidateStoredIssue(root.policyState(), config.Repositories); err != nil {
			return a.fail(err)
		}
		data, _, readErr := a.runBDCaptureWithStderr(a.dir, "--json", "epic", "child-closure", root.ID)
		if readErr != nil {
			return a.fail(fmt.Errorf("reading epic child closure: %w", readErr))
		}
		targets, err = decodeBulkChildIDs(data)
		if err != nil {
			return a.fail(fmt.Errorf("decoding epic child closure: %w", err))
		}
		filtered := targets[:0]
		for _, id := range targets {
			if id != root.ID {
				filtered = append(filtered, id)
			}
		}
		targets = filtered
	}
	targets = sortedUnique(targets)
	for _, id := range targets {
		if err := safeID("labels", id); err != nil {
			return a.fail(err)
		}
		issue, readErr := a.readIssue(id, false)
		if readErr != nil {
			return a.fail(readErr)
		}
		if issue.Status == "tombstone" {
			return a.fail(fmt.Errorf("issue %s is tombstoned", id))
		}
		if err := hub.ValidateStoredIssue(issue.policyState(), config.Repositories); err != nil {
			return a.fail(fmt.Errorf("validating issue %s: %w", id, err))
		}
	}

	labels := sortedUnique(request.labels)
	response := bulkLabelResponse{
		Operation: request.labelsOperation,
		Matched:   len(targets),
		Results:   make([]bulkLabelResult, 0, len(targets)),
	}
	attempted := false
	for _, id := range targets {
		args := []string{"--json", "update", id}
		switch request.labelsOperation {
		case "add":
			args = append(args, "--add-label", strings.Join(labels, ","))
		case "remove":
			args = append(args, "--remove-label", strings.Join(labels, ","))
		case "replace":
			if request.labelsClear {
				args = append(args, "--set-ordinary-labels=")
			} else {
				args = append(args, "--set-ordinary-labels", strings.Join(labels, ","))
			}
		}
		attempted = true
		data, _, updateErr := a.runBDCaptureWithStderr(a.dir, args...)
		var post bdIssue
		if updateErr == nil {
			post, updateErr = decodeBulkLabelPostState(data, id)
		}
		if updateErr == nil {
			if post.Status == "tombstone" {
				updateErr = errors.New("updated issue is tombstoned")
			} else {
				updateErr = hub.ValidateStoredIssue(post.policyState(), config.Repositories)
			}
		}
		if updateErr != nil {
			response.Errors++
			response.Results = append(response.Results, bulkLabelResult{
				ID: id, Status: "error", OutcomeUnknown: true,
				Error: &struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				}{Code: "backend_error", Message: updateErr.Error()},
			})
			continue
		}
		ordinary := ordinaryLabels(post.Labels)
		sort.Strings(ordinary)
		response.Applied++
		response.Results = append(response.Results, bulkLabelResult{ID: id, Status: "applied", Labels: &ordinary})
	}
	if attempted {
		a.signalMutation("bulk label mutation")
	}
	if response.Errors > 0 {
		_ = json.NewEncoder(a.stderr).Encode(map[string]any{"error": map[string]any{
			"code": "bulk_label_partial_failure", "matched": response.Matched,
			"applied": response.Applied, "errors": response.Errors,
		}})
	}
	if err := json.NewEncoder(a.stdout).Encode(response); err != nil {
		return a.fail(fmt.Errorf("writing bulk label result: %w", err))
	}
	if response.Errors > 0 {
		return 1
	}
	return 0
}

func decodeBulkChildIDs(data []byte) ([]string, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '[' {
		return nil, errors.New("epic child closure is not an array")
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for i, row := range rows {
		raw, ok := row["id"]
		if !ok {
			return nil, fmt.Errorf("child %d has no id", i)
		}
		var id string
		if err := json.Unmarshal(raw, &id); err != nil || id == "" {
			return nil, fmt.Errorf("child %d has invalid id", i)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func decodeBulkLabelPostState(data []byte, expected string) (bdIssue, error) {
	var rows []json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(data), &rows); err != nil {
		return bdIssue{}, err
	}
	if len(rows) != 1 {
		return bdIssue{}, fmt.Errorf("expected one updated issue, got %d", len(rows))
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rows[0], &fields); err != nil {
		return bdIssue{}, err
	}
	for _, required := range []string{"id", "issue_type", "status"} {
		if _, ok := fields[required]; !ok {
			return bdIssue{}, fmt.Errorf("updated issue is missing %s", required)
		}
	}
	if rawLabels, ok := fields["labels"]; ok {
		labels := bytes.TrimSpace(rawLabels)
		if len(labels) == 0 || labels[0] != '[' {
			return bdIssue{}, errors.New("updated issue labels are not an array")
		}
	}
	var post bdIssue
	if err := json.Unmarshal(rows[0], &post); err != nil {
		return bdIssue{}, err
	}
	if _, ok := fields["labels"]; !ok {
		post.Labels = []string{}
	}
	if post.ID != expected {
		return bdIssue{}, fmt.Errorf("update returned issue %s, want %s", post.ID, expected)
	}
	if post.IssueType == "" || post.Status == "" {
		return bdIssue{}, errors.New("updated issue has incomplete identity")
	}
	for _, label := range post.Labels {
		if hub.IsContextLabel(label) && !strings.HasPrefix(label, "ctx:") {
			return bdIssue{}, errors.New("invalid context label in post-state")
		}
	}
	return post, nil
}

func sortedUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
