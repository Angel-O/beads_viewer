package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

// hubScopeMemberLoader is the narrow Hub seam used by Viewer reloads. It asks
// wbd for the active named scope and its explicit members; it never infers
// membership from labels, dependencies, or related issues.
type hubScopeMemberLoader func(context.Context) ([]string, error)

// RobotActiveScope is the stable active-scope identity carried by Hub robot
// envelopes. Member IDs remain the source of truth for the bounded issue slice.
type RobotActiveScope struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CreatedOn   string `json:"created_on"`
	State       string `json:"state"`
	MemberCount int    `json:"member_count"`
	MemberLimit int    `json:"member_limit"`
}

type hubScopeSnapshot struct {
	Active    *RobotActiveScope
	MemberIDs []string
}

type hubScopeSnapshotLoader func(context.Context) (hubScopeSnapshot, error)

func newHubScopeMemberLoader(workDir string) hubScopeMemberLoader {
	load := newHubScopeSnapshotLoader(workDir)
	return func(ctx context.Context) ([]string, error) {
		snapshot, err := load(ctx)
		if err != nil {
			return nil, err
		}
		return snapshot.MemberIDs, nil
	}
}

func newHubScopeSnapshotLoader(workDir string) hubScopeSnapshotLoader {
	return func(ctx context.Context) (hubScopeSnapshot, error) {
		activeData, err := runWBDScopeCommand(ctx, workDir, "active")
		if err != nil {
			if isNoActiveScopeError(err) {
				return hubScopeSnapshot{}, nil
			}
			return hubScopeSnapshot{}, err
		}
		active, err := decodeActiveScope(activeData)
		if err != nil {
			return hubScopeSnapshot{}, err
		}
		if active == nil || active.ID == "" {
			return hubScopeSnapshot{}, nil
		}
		shown, err := runWBDScopeCommand(ctx, workDir, "show", active.ID)
		if err != nil {
			return hubScopeSnapshot{}, err
		}
		if shownScope, decodeErr := decodeActiveScope(shown); decodeErr != nil {
			return hubScopeSnapshot{}, decodeErr
		} else {
			mergeActiveScope(active, shownScope)
		}
		memberIDs, err := decodeScopeMemberIDs(shown)
		if err != nil {
			return hubScopeSnapshot{}, err
		}
		active.MemberCount = len(memberIDs)
		return hubScopeSnapshot{Active: active, MemberIDs: memberIDs}, nil
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

func decodeActiveScope(data []byte) (*RobotActiveScope, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("decoding wbd scope active: %w", err)
	}
	object := scopeObject(value)
	if object == nil {
		return nil, nil
	}
	active := &RobotActiveScope{
		ID:          firstString(object, "id", "scope_id"),
		Name:        firstString(object, "name", "scope_name"),
		CreatedOn:   firstString(object, "created_on", "created_at"),
		State:       firstString(object, "state"),
		MemberCount: firstInt(object, "member_count", "count"),
		MemberLimit: firstInt(object, "member_limit", "limit"),
	}
	if active.ID == "" {
		active.ID = namedScopeID(value)
	}
	if active.ID == "" {
		return nil, nil
	}
	if active.State == "" {
		active.State = "active"
	}
	if active.MemberLimit == 0 {
		active.MemberLimit = 100
	}
	return active, nil
}

func scopeObject(value any) map[string]any {
	switch value := value.(type) {
	case map[string]any:
		for _, key := range []string{"scope", "active_scope"} {
			if nested, ok := value[key].(map[string]any); ok {
				return nested
			}
		}
		return value
	case []any:
		if len(value) == 1 {
			return scopeObject(value[0])
		}
	}
	return nil
}

func firstString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstInt(object map[string]any, keys ...string) int {
	for _, key := range keys {
		if value, ok := object[key].(float64); ok {
			return int(value)
		}
	}
	return 0
}

func mergeActiveScope(dst, src *RobotActiveScope) {
	if dst == nil || src == nil {
		return
	}
	if dst.Name == "" {
		dst.Name = src.Name
	}
	if dst.CreatedOn == "" {
		dst.CreatedOn = src.CreatedOn
	}
	if dst.State == "" {
		dst.State = src.State
	}
	if dst.MemberCount == 0 {
		dst.MemberCount = src.MemberCount
	}
	if dst.MemberLimit == 0 {
		dst.MemberLimit = src.MemberLimit
	}
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
			bounded := issue.Clone()
			allDependencies := bounded.Dependencies
			dependencies := make([]*model.Dependency, 0, len(allDependencies))
			for _, dependency := range allDependencies {
				if dependency != nil {
					if _, ok := allowed[dependency.DependsOnID]; ok {
						dependencies = append(dependencies, dependency)
					}
				}
			}
			bounded.Dependencies = dependencies
			filtered = append(filtered, bounded)
		}
	}
	return filtered
}

// filterHubRobotSelection applies wbv's context/contextless filter after
// active-scope membership has bounded the issue slice.
func filterHubRobotSelection(issues []model.Issue, selection hub.HubScope) []model.Issue {
	if selection.Mode == hub.HubScopeAllItems {
		return issues
	}
	memberIDs := make([]string, 0, len(issues))
	for _, issue := range issues {
		if selection.MatchesLabels(issue.Labels) {
			memberIDs = append(memberIDs, issue.ID)
		}
	}
	return filterHubScopeIssues(issues, memberIDs)
}
