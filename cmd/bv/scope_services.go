package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/ui"
)

// newHubScopeServices adapts the stable wbd JSON forwarding surface to the
// Viewer. The UI never shells out directly and never infers membership.
func newHubScopeServices(workDir string) ui.ScopeServices {
	loadBacklog := func(ctx context.Context, query ui.BacklogQuery) (ui.BacklogPage, error) {
		args := make([]string, 0, 9)
		if strings.TrimSpace(query.Label) != "" {
			args = append(args, "--label", query.Label)
		}
		if strings.TrimSpace(query.Filter) != "" {
			args = append(args, "--filter", query.Filter)
		}
		if status := strings.TrimSpace(query.Status); status != "" && status != "all" {
			args = append(args, "--status", status)
		}
		// No context flags deliberately means all contexts, matching wbd's default.
		for _, contextID := range query.Contexts {
			args = append(args, "--context", contextID)
		}
		if query.IncludeContextless {
			args = append(args, "--contextless")
		}
		args = append(args, "--limit", strconv.Itoa(query.Limit))
		if query.Cursor != "" {
			args = append(args, "--cursor", query.Cursor)
		}
		data, err := runWBDBacklogCommand(ctx, workDir, args...)
		if err != nil {
			return ui.BacklogPage{}, err
		}
		return decodeBacklogPage(data)
	}
	return ui.ScopeServices{
		QueryCatalog: func(ctx context.Context, query ui.ScopeCatalogQuery) (ui.ScopeCatalogPage, error) {
			args := []string{"--paginate", "--limit", strconv.Itoa(query.Limit)}
			if query.Cursor != "" {
				args = append(args, "--cursor", query.Cursor)
			}
			data, err := runWBDScopeCommand(ctx, workDir, "list", args...)
			if err != nil {
				return ui.ScopeCatalogPage{}, err
			}
			return decodeScopeCatalogPage(data, query.Limit)
		},
		QueryMembers: func(ctx context.Context, query ui.ScopeMembersQuery) (ui.ScopeMembersPage, error) {
			args := []string{"--paginate", "--limit", strconv.Itoa(query.Limit)}
			if query.Cursor != "" {
				args = append(args, "--cursor", query.Cursor)
			}
			if query.Status != "" {
				args = append(args, "--status", query.Status)
			}
			if query.Type != "" {
				args = append(args, "--type", query.Type)
			}
			for _, contextID := range query.Contexts {
				args = append(args, "--context", contextID)
			}
			data, err := runWBDScopeCommand(ctx, workDir, "show", append([]string{query.ScopeID}, args...)...)
			if err != nil {
				return ui.ScopeMembersPage{}, err
			}
			return decodeScopeMembersPage(data, query.Limit, query.ScopeID)
		},
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
		Create: func(ctx context.Context, name string) error {
			id := scopeIDFromName(name)
			if id == "" {
				return fmt.Errorf("scope name must contain a letter or number")
			}
			_, err := runWBDScopeCommand(ctx, workDir, "create", id, name)
			return err
		},
		Activate: func(ctx context.Context, id string) error {
			_, err := runWBDScopeCommand(ctx, workDir, "activate", id)
			return err
		},
		Deactivate: func(ctx context.Context) error {
			_, err := runWBDScopeCommand(ctx, workDir, "deactivate")
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
		QueryBacklog: loadBacklog,
		LoadDetails: func(ctx context.Context, scopeID string) (ui.ScopeDetails, error) {
			data, err := runWBDScopeCommand(ctx, workDir, "show", scopeID)
			if err != nil {
				return ui.ScopeDetails{}, err
			}
			return decodeScopeDetails(data, scopeID)
		},
		Mutate: func(ctx context.Context, mutation ui.ScopeMutation) error {
			return runHubScopeMutation(ctx, workDir, mutation, false)
		},
		MutateMatching: func(ctx context.Context, mutation ui.ScopeMutation) error {
			return runHubScopeMutation(ctx, workDir, mutation, true)
		},
		LoadBacklog: func(ctx context.Context, cursor string, limit int) (ui.BacklogPage, error) {
			return loadBacklog(ctx, ui.BacklogQuery{Cursor: cursor, Limit: limit})
		},
	}
}

// runHubScopeMutation translates the typed UI operation to one wbd scope call.
func runHubScopeMutation(ctx context.Context, workDir string, mutation ui.ScopeMutation, matching bool) error {
	var args []string
	switch mutation.Kind {
	case ui.ScopeMutationCreate:
		id := scopeIDFromName(mutation.Name)
		if id == "" {
			return fmt.Errorf("scope name must contain a letter or number")
		}
		args = []string{"create", id, mutation.Name}
	case ui.ScopeMutationActivate:
		args = []string{"activate", mutation.ScopeID}
	case ui.ScopeMutationDeactivate:
		args = []string{"deactivate"}
	case ui.ScopeMutationAdd, ui.ScopeMutationRemove:
		action := string(mutation.Kind)
		args = []string{action}
		if matching {
			switch {
			case mutation.EpicID != "" && mutation.Label != "":
				return fmt.Errorf("scope mutation accepts one semantic target")
			case mutation.EpicID != "":
				args = append(args, "--epic", mutation.EpicID)
			case mutation.Label != "":
				args = append(args, "--label", mutation.Label)
			default:
				return fmt.Errorf("scope mutation requires an epic or label target")
			}
		} else {
			if len(mutation.IssueIDs) == 0 {
				return fmt.Errorf("scope mutation requires an issue ID")
			}
			args = append(args, mutation.IssueIDs...)
		}
		if mutation.ScopeID != "" {
			args = append(args, "--scope", mutation.ScopeID)
		}
	case ui.ScopeMutationMove:
		if len(mutation.IssueIDs) == 0 {
			return fmt.Errorf("scope move requires an issue ID")
		}
		args = append([]string{"move"}, mutation.IssueIDs...)
		if mutation.SourceScopeID != "" {
			args = append(args, "--source-scope", mutation.SourceScopeID)
		}
		if mutation.TargetScopeID != "" {
			args = append(args, "--target-scope", mutation.TargetScopeID)
		}
	default:
		return fmt.Errorf("unsupported scope mutation %q", mutation.Kind)
	}
	_, err := runWBDScopeCommand(ctx, workDir, args[0], args[1:]...)
	return err
}

// scopeIDFromName supplies the backend ID for the UI's name-only creation
// prompt. IDs are stable slugs; activation remains a separate user action.
func scopeIDFromName(name string) string {
	var builder strings.Builder
	dashed := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			dashed = false
		} else if builder.Len() > 0 && !dashed {
			builder.WriteByte('-')
			dashed = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func runWBDBacklogCommand(ctx context.Context, workDir string, args ...string) ([]byte, error) {
	commandArgs := append([]string{"backlog", "list"}, args...)
	commandArgs = append(commandArgs, "--json")
	command := exec.CommandContext(ctx, "wbd", commandArgs...)
	command.Dir = workDir
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("wbd backlog failed: %s", detail)
	}
	return stdout.Bytes(), nil
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

type scopePaginationWire struct {
	// SchemaVersion is emitted by bd on paginated scope envelopes.
	SchemaVersion int     `json:"schema_version"`
	Limit         *int    `json:"limit"`
	ReturnedCount *int    `json:"returned_count"`
	TotalMatching *int    `json:"total_matching"`
	HasMore       *bool   `json:"has_more"`
	NextCursor    *string `json:"next_cursor"`
}

type scopePageMetadata struct {
	HasMore    bool
	NextCursor string
}

type scopeCatalogEnvelope struct {
	Items json.RawMessage `json:"items"`
	scopePaginationWire
}

type scopeMembersEnvelope struct {
	Scope          json.RawMessage `json:"scope"`
	Members        json.RawMessage `json:"members"`
	MemberCount    *int            `json:"member_count"`
	CompletedCount *int            `json:"completed_count"`
	scopePaginationWire
}

func decodeScopeCatalogPage(data []byte, expectedLimit int) (ui.ScopeCatalogPage, error) {
	var envelope scopeCatalogEnvelope
	if err := decodeStrictJSON(data, &envelope); err != nil {
		return ui.ScopeCatalogPage{}, fmt.Errorf("decoding wbd scope list: %w", err)
	}
	if len(envelope.Items) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Items), []byte("null")) {
		return ui.ScopeCatalogPage{}, fmt.Errorf("decoding wbd scope list: response must contain items")
	}
	var rawScopes []json.RawMessage
	if err := json.Unmarshal(envelope.Items, &rawScopes); err != nil {
		return ui.ScopeCatalogPage{}, fmt.Errorf("decoding wbd scope list scopes: %w", err)
	}
	scopes := make([]ui.ScopeInfo, 0, len(rawScopes))
	for index, raw := range rawScopes {
		var object map[string]any
		if err := json.Unmarshal(raw, &object); err != nil || object == nil {
			if err == nil {
				err = fmt.Errorf("scope row must be an object")
			}
			return ui.ScopeCatalogPage{}, fmt.Errorf("decoding wbd scope list scope %d: %w", index, err)
		}
		decoded, err := decodeScopeInfoObject(object)
		if err != nil {
			return ui.ScopeCatalogPage{}, fmt.Errorf("decoding wbd scope list scope %d: %w", index, err)
		}
		scopes = append(scopes, decoded)
	}
	pagination, err := decodeScopePagination(envelope.scopePaginationWire, expectedLimit, len(scopes))
	if err != nil {
		return ui.ScopeCatalogPage{}, fmt.Errorf("decoding wbd scope list pagination: %w", err)
	}
	return ui.ScopeCatalogPage{Scopes: scopes, HasMore: pagination.HasMore, NextCursor: pagination.NextCursor}, nil
}

func decodeScopeMembersPage(data []byte, expectedLimit int, scopeID string) (ui.ScopeMembersPage, error) {
	var envelope scopeMembersEnvelope
	if err := decodeStrictJSON(data, &envelope); err != nil {
		return ui.ScopeMembersPage{}, fmt.Errorf("decoding wbd scope show: %w", err)
	}
	if len(envelope.Members) == 0 {
		return ui.ScopeMembersPage{}, fmt.Errorf("decoding wbd scope show: response must contain members")
	}
	var members []model.Issue
	if bytes.Equal(bytes.TrimSpace(envelope.Members), []byte("null")) {
		members = []model.Issue{}
	} else if err := json.Unmarshal(envelope.Members, &members); err != nil {
		return ui.ScopeMembersPage{}, fmt.Errorf("decoding wbd scope show members: %w", err)
	}
	if envelope.MemberCount == nil || envelope.CompletedCount == nil {
		return ui.ScopeMembersPage{}, fmt.Errorf("decoding wbd scope show: response must contain member_count and completed_count")
	}
	if *envelope.MemberCount < 0 || *envelope.CompletedCount < 0 || *envelope.CompletedCount > *envelope.MemberCount {
		return ui.ScopeMembersPage{}, fmt.Errorf("decoding wbd scope show: invalid member counts")
	}
	if len(envelope.Scope) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Scope), []byte("null")) {
		return ui.ScopeMembersPage{}, fmt.Errorf("decoding wbd scope show: response must contain scope")
	}
	var object map[string]any
	if err := json.Unmarshal(envelope.Scope, &object); err != nil || object == nil {
		if err == nil {
			err = fmt.Errorf("scope must be an object")
		}
		return ui.ScopeMembersPage{}, fmt.Errorf("decoding wbd scope show scope: %w", err)
	}
	info, err := decodeScopeInfoObject(object)
	if err != nil {
		return ui.ScopeMembersPage{}, fmt.Errorf("decoding wbd scope show scope: %w", err)
	}
	info.MemberCount = *envelope.MemberCount
	if info.ID == "" {
		info.ID = scopeID
	}
	pagination, err := decodeScopePagination(envelope.scopePaginationWire, expectedLimit, len(members))
	if err != nil {
		return ui.ScopeMembersPage{}, fmt.Errorf("decoding wbd scope show pagination: %w", err)
	}
	return ui.ScopeMembersPage{Scope: info, Members: members, HasMore: pagination.HasMore, NextCursor: pagination.NextCursor}, nil
}

func decodeScopeInfoObject(object map[string]any) (ui.ScopeInfo, error) {
	info := ui.ScopeInfo{ID: firstString(object, "id", "scope_id"), Name: firstString(object, "name", "scope_name"), MemberCount: firstInt(object, "member_count", "count")}
	if created := firstString(object, "created_at", "created_on"); created != "" {
		parsed, err := time.Parse(time.RFC3339, created)
		if err != nil {
			return ui.ScopeInfo{}, fmt.Errorf("invalid created_at %q: %w", created, err)
		}
		info.CreatedAt = parsed
	}
	info.Active = strings.EqualFold(firstString(object, "state"), "active")
	return info, nil
}

func decodeScopePagination(pagination scopePaginationWire, expectedLimit, returnedCount int) (scopePageMetadata, error) {
	if pagination.Limit == nil || pagination.ReturnedCount == nil || pagination.TotalMatching == nil || pagination.HasMore == nil {
		return scopePageMetadata{}, fmt.Errorf("pagination must contain limit, returned_count, total_matching, and has_more")
	}
	if *pagination.Limit != expectedLimit {
		return scopePageMetadata{}, fmt.Errorf("pagination limit is %d, want %d", *pagination.Limit, expectedLimit)
	}
	if *pagination.ReturnedCount != returnedCount {
		return scopePageMetadata{}, fmt.Errorf("pagination returned_count is %d, want %d", *pagination.ReturnedCount, returnedCount)
	}
	if *pagination.TotalMatching < *pagination.ReturnedCount {
		return scopePageMetadata{}, fmt.Errorf("pagination total_matching is %d, less than returned_count %d", *pagination.TotalMatching, *pagination.ReturnedCount)
	}
	if *pagination.HasMore && (pagination.NextCursor == nil || *pagination.NextCursor == "") {
		return scopePageMetadata{}, fmt.Errorf("pagination with has_more=true must contain next_cursor")
	}
	if !*pagination.HasMore && pagination.NextCursor != nil && *pagination.NextCursor != "" {
		return scopePageMetadata{}, fmt.Errorf("terminal pagination must not contain next_cursor")
	}
	result := scopePageMetadata{HasMore: *pagination.HasMore}
	if pagination.NextCursor != nil {
		result.NextCursor = *pagination.NextCursor
	}
	return result, nil
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("response contains multiple JSON values")
		}
		return err
	}
	return nil
}

// decodeScopeDetails keeps scope membership and any full issue projection from wbd together.
func decodeScopeDetails(data []byte, scopeID string) (ui.ScopeDetails, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return ui.ScopeDetails{}, fmt.Errorf("decoding wbd scope show: %w", err)
	}
	object := scopeObject(value)
	if object == nil {
		return ui.ScopeDetails{}, fmt.Errorf("decoding wbd scope show: expected an object")
	}
	info := ui.ScopeInfo{
		ID:          firstString(object, "id", "scope_id"),
		Name:        firstString(object, "name", "scope_name"),
		MemberCount: firstInt(object, "member_count", "count"),
	}
	if info.ID == "" {
		info.ID = scopeID
	}
	if created := firstString(object, "created_at", "created_on"); created != "" {
		info.CreatedAt, _ = time.Parse(time.RFC3339, created)
	}
	info.Active = strings.EqualFold(firstString(object, "state"), "active")
	memberIDs, err := decodeScopeMemberIDs(data)
	if err != nil {
		return ui.ScopeDetails{}, err
	}
	issues := decodeScopeDetailIssues(object)
	if info.MemberCount == 0 {
		info.MemberCount = len(memberIDs)
	}
	return ui.ScopeDetails{Info: info, Issues: issues, MemberIDs: memberIDs}, nil
}

func decodeScopeDetailIssues(object map[string]any) []model.Issue {
	for _, key := range []string{"issues", "beads", "items", "members"} {
		value, ok := object[key]
		if !ok {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			continue
		}
		var issues []model.Issue
		if json.Unmarshal(encoded, &issues) == nil {
			return issues
		}
	}
	return nil
}
