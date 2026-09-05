package main

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
)

// Scope and backlog JSON are deliberately the stabilized bd JSON contracts:
// wbd forwards successful JSON bytes rather than decoding and re-encoding an
// evolving backend schema. Human output is likewise bd's stable text output.

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
	args := appendJSON(nil, request.json)
	args = append(args, "list", "--unscoped")
	if request.json {
		// JSON backlog output is the bounded, stable page contract used by wbd
		// list. The cursor remains opaque and is forwarded byte-for-byte.
		args = append(args, "--paginate", "--sort", hub.BackendListSort("updated_at:desc"))
	}
	args = append(args, request.args...)
	return a.runBD(a.dir, args...)
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
