package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/ui"
)

// newHubScopeServices adapts the stable wbd JSON forwarding surface to the
// Viewer. The UI never shells out directly and never infers membership.
func newHubScopeServices(workDir string) ui.ScopeServices {
	return ui.ScopeServices{
		Load: func(ctx context.Context) (ui.ScopeSnapshot, error) {
			listData, err := runWBDScopeCommand(ctx, workDir, "list")
			if err != nil {
				return ui.ScopeSnapshot{}, err
			}
			scopes, err := decodeScopeInfos(listData)
			if err != nil {
				return ui.ScopeSnapshot{}, err
			}
			activeData, activeErr := runWBDScopeCommand(ctx, workDir, "active")
			if activeErr == nil {
				activeID, err := decodeNamedScopeID(activeData)
				if err != nil {
					return ui.ScopeSnapshot{}, err
				}
				for i := range scopes {
					if scopes[i].ID == activeID || scopes[i].Name == activeID {
						scopes[i].Active = true
						active := scopes[i]
						return ui.ScopeSnapshot{Scopes: scopes, Active: &active}, nil
					}
				}
			} else if !isNoActiveScopeError(activeErr) {
				return ui.ScopeSnapshot{}, activeErr
			}
			return ui.ScopeSnapshot{Scopes: scopes}, nil
		},
		Activate: func(ctx context.Context, id string) error {
			_, err := runWBDScopeCommand(ctx, workDir, "activate", id)
			return err
		},
		Add: func(ctx context.Context, issueID, scopeID string) error {
			_, err := runWBDScopeCommand(ctx, workDir, "add", issueID, "--scope", scopeID)
			return err
		},
		Remove: func(ctx context.Context, issueID, scopeID string) error {
			_, err := runWBDScopeCommand(ctx, workDir, "remove", issueID, "--scope", scopeID)
			return err
		},
		Move: func(ctx context.Context, issueID, sourceID, targetID string) error {
			_, err := runWBDScopeCommand(ctx, workDir, "move", issueID, "--source-scope", sourceID, "--target-scope", targetID)
			return err
		},
		LoadBacklog: func(ctx context.Context, cursor string, limit int) (ui.BacklogPage, error) {
			args := []string{"--limit", strconv.Itoa(limit)}
			if cursor != "" {
				args = append(args, "--cursor", cursor)
			}
			data, err := runWBDBacklogCommand(ctx, workDir, args...)
			if err != nil {
				return ui.BacklogPage{}, err
			}
			return decodeBacklogPage(data)
		},
	}
}

func runWBDBacklogCommand(ctx context.Context, workDir string, args ...string) ([]byte, error) {
	commandArgs := append([]string{"backlog", "list"}, args...)
	commandArgs = append(commandArgs, "--json")
	command := exec.CommandContext(ctx, "wbd", commandArgs...)
	command.Dir = workDir
	output, err := command.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("wbd backlog failed: %s", detail)
	}
	return output, nil
}

func decodeScopeInfos(data []byte) ([]ui.ScopeInfo, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("decoding wbd scope list: %w", err)
	}
	var raw []any
	switch object := value.(type) {
	case []any:
		raw = object
	case map[string]any:
		if scopes, ok := object["scopes"].([]any); ok {
			raw = scopes
		}
	}
	result := make([]ui.ScopeInfo, 0, len(raw))
	for _, item := range raw {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		info := ui.ScopeInfo{}
		for _, key := range []string{"id", "scope_id"} {
			if value, ok := object[key].(string); ok {
				info.ID = value
				break
			}
		}
		for _, key := range []string{"name", "scope_name"} {
			if value, ok := object[key].(string); ok {
				info.Name = value
				break
			}
		}
		for _, key := range []string{"member_count", "count"} {
			if value, ok := object[key].(float64); ok {
				info.MemberCount = int(value)
				break
			}
		}
		for _, key := range []string{"created_at", "created_on"} {
			if value, ok := object[key].(string); ok {
				info.CreatedAt, _ = time.Parse(time.RFC3339, value)
				break
			}
		}
		if info.ID != "" || info.Name != "" {
			result = append(result, info)
		}
	}
	return result, nil
}

func decodeBacklogPage(data []byte) (ui.BacklogPage, error) {
	var envelope struct {
		Issues     []model.Issue `json:"issues"`
		Pagination struct {
			HasMore    bool   `json:"has_more"`
			NextCursor string `json:"next_cursor"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return ui.BacklogPage{}, fmt.Errorf("decoding wbd backlog: %w", err)
	}
	return ui.BacklogPage{Issues: envelope.Issues, HasMore: envelope.Pagination.HasMore, NextCursor: envelope.Pagination.NextCursor}, nil
}
