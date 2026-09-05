package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
		name, err := a.scopeOption(request.args, "--scope")
		if err != nil {
			return a.fail(err)
		}
		issues := request.positionals
		if name == "" {
			name, err = a.activeScope()
			if err != nil {
				return a.fail(err)
			}
		}
		args = append(args, name)
		args = append(args, issues...)
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
