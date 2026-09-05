package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
)

const backlogContextLabelPrefix = "ctx:"

// Scope JSON is deliberately bd's stabilized contract: wbd forwards successful
// JSON bytes rather than decoding and re-encoding an evolving backend schema.
// Backlog pages use the shared stable issue projection and cursor validation.

func (a *app) scope(request request) int {
	args := appendJSON(nil, request.json)
	args = append(args, "scope", request.scopeSubcommand)

	switch request.scopeSubcommand {
	case "create", "show":
		args = append(args, request.positionals...)
		args = append(args, request.args...)
	case "list", "active":
		args = append(args, request.args...)
	case "activate":
		args = append(args, request.positionals...)
		args = append(args, request.args...)
	case "deactivate":
		args = append(args, request.args...)
	case "add", "remove":
		return a.semanticScopeMutation(request)
	default: // move
		source, err := a.scopeOption(request.args, "--source-scope")
		if err != nil {
			return a.fail(err)
		}
		target, err := a.scopeOption(request.args, "--target-scope")
		if err != nil {
			return a.fail(err)
		}
		issues := request.positionals
		if source == "" || target == "" {
			active, activeErr := a.activeScope()
			if activeErr != nil {
				return a.fail(activeErr)
			}
			if source == "" {
				source = active
			}
			if target == "" {
				target = active
			}
		}
		args = append(args, source, target)
		args = append(args, issues...)
	}

	if request.scopeSubcommand == "create" || request.scopeSubcommand == "activate" ||
		request.scopeSubcommand == "deactivate" || request.scopeSubcommand == "add" ||
		request.scopeSubcommand == "remove" || request.scopeSubcommand == "move" {
		return a.runBDMutation(a.dir, args...)
	}
	return a.runBD(a.dir, args...)
}

// semanticScopeMutation resolves one exact selector target to IDs, then preserves the
// backend's existing multi-ID scope mutation. Reads are intentionally bounded
// to the current repository and requested status/type; add starts unscoped,
// while remove starts with members of the selected scope.
func (a *app) semanticScopeMutation(request request) int {
	name, err := a.scopeOption(request.args, "--scope")
	if err != nil {
		return a.fail(err)
	}
	if name == "" {
		name, err = a.activeScope()
		if err != nil {
			return a.fail(err)
		}
	}
	context, err := hub.Context(a.dir)
	if err != nil {
		return a.fail(err)
	}

	selected := map[string]struct{}(nil)
	if request.scopeSubcommand == "remove" {
		selected, err = a.scopeMemberIDs(name)
		if err != nil {
			return a.fail(err)
		}
		if len(selected) == 0 {
			return a.writeScopeMutationCount(request, name, 0)
		}
	}

	args := []string{"--json", "list"}
	if request.scopeSubcommand == "add" {
		args = append(args, "--unscoped")
	} else {
		ids := make([]string, 0, len(selected))
		for id := range selected {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		args = append(args, "--id", strings.Join(ids, ","))
	}
	args = append(args, "--limit", "0", "--label", context)
	targetIDs := scopeTargetIDs(request)
	if request.scopeSubcommand == "add" && len(targetIDs) > 0 {
		args = append(args, "--id", strings.Join(targetIDs, ","))
	} else if request.scopeLabel != "" {
		args = append(args, "--label", request.scopeLabel)
	}
	if request.scopeStatus != "" {
		args = append(args, "--status", request.scopeStatus)
	}
	if request.scopeType != "" {
		args = append(args, "--type", request.scopeType)
	}

	data, _, err := a.runBDCaptureWithStderr(a.dir, args...)
	if err != nil {
		return a.fail(err)
	}
	issues, err := decodeScopeIssues(data)
	if err != nil {
		return a.fail(fmt.Errorf("decoding scope candidates: %w", err))
	}
	var descendants map[string]struct{}
	if request.scopeEpic != "" {
		relationshipData, _, relationshipErr := a.runBDCaptureWithStderr(a.dir, "--json", "list", "--all", "--include-all-types", "--limit", "0", "--label", context)
		if relationshipErr != nil {
			return a.fail(relationshipErr)
		}
		relationships, decodeErr := decodeScopeParentIssues(relationshipData)
		if decodeErr != nil {
			return a.fail(fmt.Errorf("decoding epic relationships: %w", decodeErr))
		}
		descendants = epicDescendantIDs(relationships, request.scopeEpic)
	}
	targetSet := make(map[string]struct{}, len(targetIDs))
	for _, id := range targetIDs {
		targetSet[id] = struct{}{}
	}
	ids := make([]string, 0, len(issues))
	for _, issue := range issues {
		if request.scopeSubcommand == "remove" {
			if _, ok := selected[issue.ID]; !ok {
				continue
			}
		}
		if len(targetSet) > 0 {
			if _, ok := targetSet[issue.ID]; !ok {
				continue
			}
		}
		if descendants != nil {
			if _, ok := descendants[issue.ID]; !ok {
				continue
			}
		}
		ids = append(ids, issue.ID)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return a.writeScopeMutationCount(request, name, 0)
	}

	mutation := appendJSON(nil, request.json)
	mutation = append(mutation, "scope", request.scopeSubcommand, name)
	mutation = append(mutation, ids...)
	stdout := a.stdout
	a.stdout = io.Discard
	code := a.runBD(a.dir, mutation...)
	a.stdout = stdout
	if code != 0 {
		return code
	}
	a.signalMutation("scope mutation")
	return a.writeScopeMutationCount(request, name, len(ids))
}

func scopeTargetIDs(request request) []string {
	if request.scopeID != "" {
		return []string{request.scopeID}
	}
	return append([]string(nil), request.positionals...)
}

type scopeParentIssue struct {
	ID     string `json:"id"`
	Parent string `json:"parent"`
}

func epicDescendantIDs(issues []scopeParentIssue, root string) map[string]struct{} {
	children := make(map[string][]string)
	for _, issue := range issues {
		if issue.ID != "" && issue.Parent != "" {
			children[issue.Parent] = append(children[issue.Parent], issue.ID)
		}
	}
	seen := map[string]struct{}{root: {}}
	descendants := make(map[string]struct{})
	queue := []string{root}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, child := range children[parent] {
			if child == "" {
				continue
			}
			if _, ok := seen[child]; ok {
				continue
			}
			seen[child] = struct{}{}
			descendants[child] = struct{}{}
			queue = append(queue, child)
		}
	}
	return descendants
}

func (a *app) scopeMemberIDs(name string) (map[string]struct{}, error) {
	data, err := a.runBDCapture(a.dir, "--json", "scope", "show", name)
	if err != nil {
		return nil, err
	}
	return decodeScopeMemberIDs(data)
}

func decodeScopeIssues(data []byte) ([]bdIssue, error) {
	var issues []bdIssue
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '[' {
		if err := json.Unmarshal(data, &issues); err != nil {
			return nil, err
		}
		return issues, nil
	}
	var envelope struct {
		Issues []bdIssue `json:"issues"`
		Items  []bdIssue `json:"items"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	if envelope.Issues != nil {
		return envelope.Issues, nil
	}
	return envelope.Items, nil
}

func decodeScopeParentIssues(data []byte) ([]scopeParentIssue, error) {
	var issues []scopeParentIssue
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '[' {
		if err := json.Unmarshal(data, &issues); err != nil {
			return nil, err
		}
		return issues, nil
	}
	var envelope struct {
		Issues []scopeParentIssue `json:"issues"`
		Items  []scopeParentIssue `json:"items"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	if envelope.Issues != nil {
		return envelope.Issues, nil
	}
	return envelope.Items, nil
}

func decodeScopeMemberIDs(data []byte) (map[string]struct{}, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	ids := make(map[string]struct{})
	var visit func(any, bool)
	visit = func(value any, member bool) {
		switch value := value.(type) {
		case string:
			if member && value != "" {
				ids[value] = struct{}{}
			}
		case []any:
			for _, item := range value {
				visit(item, member)
			}
		case map[string]any:
			for key, item := range value {
				switch key {
				case "issues", "items", "members", "issue_ids":
					visit(item, true)
				case "id", "issue_id":
					if member {
						visit(item, true)
					}
				}
			}
		}
	}
	if _, ok := value.([]any); ok {
		visit(value, true)
	} else {
		visit(value, false)
	}
	return ids, nil
}

func (a *app) writeScopeMutationCount(request request, scope string, count int) int {
	if request.json {
		key := "added"
		if request.scopeSubcommand == "remove" {
			key = "removed"
		}
		return a.writeJSON(map[string]int{key: count})
	}
	verb := "Added"
	preposition := "to"
	if request.scopeSubcommand == "remove" {
		verb = "Removed"
		preposition = "from"
	}
	fmt.Fprintf(a.stdout, "%s %d issues %s %s\n", verb, count, preposition, scope)
	return 0
}

func (a *app) backlog(request request) int {
	if len(request.backlogContexts) > 0 {
		config, err := hub.Resolve(a.paths.Config)
		if err != nil {
			return a.fail(err)
		}
		if err := hub.ValidateRegisteredContexts(request.backlogContexts, config.Repositories); err != nil {
			return a.fail(err)
		}
	}

	args := appendJSON(nil, request.json)
	args = append(args, "list", "--unscoped")
	if len(request.backlogContexts) > 0 {
		args = append(args, "--label-any", strings.Join(request.backlogContexts, ","))
	}
	if request.backlogContextless {
		args = append(args, "--or-no-label-prefix", backlogContextLabelPrefix)
	}
	if request.backlogStatus != "" {
		args = append(args, "--status", request.backlogStatus)
	}
	if request.backlogType != "" {
		args = append(args, "--type", request.backlogType)
	}
	if request.backlogSort != "" {
		args = append(args, "--sort", hub.BackendListSort(request.backlogSort))
	} else if request.json {
		args = append(args, "--sort", hub.BackendListSort(backlogSort(request.backlogSort)))
	}
	if request.json {
		// Filters are delegated so bd applies them before keyset pagination. The
		// cursor is opaque and is forwarded byte-for-byte.
		args = append(args, "--paginate")
	}
	if request.backlogLimit > 0 {
		args = append(args, "--limit", strconv.Itoa(request.backlogLimit))
	}
	if request.backlogCursor != "" {
		args = append(args, "--cursor", request.backlogCursor)
	}
	if !request.json {
		return a.runBD(a.dir, args...)
	}

	data, childStderr, err := a.runBDCaptureWithStderr(a.dir, args...)
	if err != nil {
		return a.fail(fmt.Errorf("listing backlog issues: %w", err))
	}
	response, err := hub.DecodeListResponse(data, true, request.backlogLimit, false)
	if err != nil {
		if len(childStderr) > 0 {
			_, _ = a.stderr.Write(childStderr)
		}
		return a.fail(fmt.Errorf("decoding bd backlog response: %w", err))
	}
	if _, err := a.stdout.Write(response); err != nil {
		return a.fail(fmt.Errorf("writing backlog response: %w", err))
	}
	if len(childStderr) > 0 {
		if _, err := a.stderr.Write(childStderr); err != nil {
			return a.fail(fmt.Errorf("writing backlog diagnostics: %w", err))
		}
	}
	return 0
}

func backlogSort(value string) string {
	if value == "" {
		return "updated_at:desc"
	}
	return value
}

func (a *app) scopeOption(arguments []string, flag string) (string, error) {
	for index := 0; index+1 < len(arguments); index += 2 {
		if arguments[index] == flag {
			return arguments[index+1], nil
		}
	}
	return "", nil
}

func (a *app) activeScope() (string, error) {
	data, err := a.runBDCapture(a.dir, "--json", "scope", "active")
	if err != nil {
		return "", err
	}
	name := decodeActiveScope(data)
	if name == "" {
		return "", errors.New("bd scope active returned no active scope ID")
	}
	return name, nil
}

func decodeActiveScope(data []byte) string {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	if decoder.Decode(&value) != nil {
		return ""
	}
	return activeScopeValue(value)
}

func activeScopeValue(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case map[string]any:
		for _, key := range []string{"id", "scope_id", "name", "scope", "scope_name", "active_scope"} {
			if name, ok := value[key].(string); ok && name != "" {
				return name
			}
		}
		if nested, ok := value["scope"].(map[string]any); ok {
			return activeScopeValue(nested)
		}
	}
	return ""
}
