package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// hubScopeMemberLoader is the narrow Hub seam used by Viewer reloads. It asks
// wbd for the active named scope and its explicit members; it never infers
// membership from labels, dependencies, or related issues.
type hubScopeMemberLoader func(context.Context) ([]string, error)

func newHubScopeMemberLoader(workDir string) hubScopeMemberLoader {
	return func(ctx context.Context) ([]string, error) {
		active, err := runWBDScopeCommand(ctx, workDir, "active")
		if err != nil {
			if isNoActiveScopeError(err) {
				return nil, nil
			}
			return nil, err
		}
		scopeID, err := decodeNamedScopeID(active)
		if err != nil {
			return nil, err
		}
		if scopeID == "" {
			return nil, nil
		}
		shown, err := runWBDScopeCommand(ctx, workDir, "show", scopeID)
		if err != nil {
			return nil, err
		}
		return decodeScopeMemberIDs(shown)
	}
}

func runWBDScopeCommand(ctx context.Context, workDir, subcommand string, args ...string) ([]byte, error) {
	commandArgs := []string{"scope", subcommand}
	commandArgs = append(commandArgs, args...)
	commandArgs = append(commandArgs, "--json")
	command := exec.CommandContext(ctx, "wbd", commandArgs...)
	command.Dir = workDir
	output, err := command.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("wbd scope %s failed: %s", subcommand, detail)
	}
	return output, nil
}

func isNoActiveScopeError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no active scope") ||
		strings.Contains(message, "active scope not found")
}

func decodeNamedScopeID(data []byte) (string, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return "", fmt.Errorf("decoding wbd scope active: %w", err)
	}
	return namedScopeID(value), nil
}

func namedScopeID(value any) string {
	switch value := value.(type) {
	case string:
		return strings.TrimSpace(value)
	case map[string]any:
		for _, key := range []string{"id", "scope_id", "name", "scope_name", "scope", "active_scope"} {
			if candidate, ok := value[key].(string); ok && strings.TrimSpace(candidate) != "" {
				return strings.TrimSpace(candidate)
			}
		}
		if nested, ok := value["scope"]; ok {
			return namedScopeID(nested)
		}
		if nested, ok := value["active_scope"]; ok {
			return namedScopeID(nested)
		}
	}
	return ""
}

func decodeScopeMemberIDs(data []byte) ([]string, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("decoding wbd scope show: %w", err)
	}
	ids := make(map[string]struct{})
	collectScopeMembers(value, ids)
	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Strings(result)
	return result, nil
}

func collectScopeMembers(value any, ids map[string]struct{}) {
	if _, ok := value.([]any); ok {
		collectMemberValues(value, ids)
		return
	}
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	for _, key := range []string{"issue_ids", "bead_ids", "member_ids", "members", "issues", "beads", "items"} {
		if members, exists := object[key]; exists {
			collectMemberValues(members, ids)
		}
	}
	if nested, ok := object["scope"]; ok {
		collectScopeMembers(nested, ids)
	}
}

func collectMemberValues(value any, ids map[string]struct{}) {
	switch value := value.(type) {
	case []any:
		for _, member := range value {
			collectMemberValues(member, ids)
		}
	case string:
		if id := strings.TrimSpace(value); id != "" {
			ids[id] = struct{}{}
		}
	case map[string]any:
		for _, key := range []string{"id", "issue_id", "bead_id"} {
			if id, ok := value[key].(string); ok && strings.TrimSpace(id) != "" {
				ids[strings.TrimSpace(id)] = struct{}{}
				return
			}
		}
	}
}

func filterHubScopeIssues(issues []model.Issue, memberIDs []string) []model.Issue {
	allowed := make(map[string]struct{}, len(memberIDs))
	for _, id := range memberIDs {
		allowed[id] = struct{}{}
	}
	filtered := make([]model.Issue, 0, len(allowed))
	for _, issue := range issues {
		if _, ok := allowed[issue.ID]; ok {
			filtered = append(filtered, issue)
		}
	}
	return filtered
}
