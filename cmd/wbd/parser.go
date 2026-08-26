package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type optionSpec struct {
	name        string
	value       string
	description string
	defaultText string
	allowEmpty  bool
}

type commandSpec struct {
	path     string
	usage    string
	summary  string
	options  []optionSpec
	examples []string
}

var commandOrder = []string{
	"bootstrap", "configure", "register", "context", "create", "new", "replace",
	"compatibility", "list", "show", "update", "dep", "dep add", "dep remove",
	"close", "reopen", "comments", "comments add", "link", "unlink",
}

var commandSpecs = map[string]commandSpec{
	"bootstrap": {
		path: "bootstrap", usage: "wbd bootstrap [--prefix <prefix>]", summary: "Create or repair the private Hub store.",
		options: []optionSpec{{name: "--prefix", value: "<prefix>", description: "Issue ID prefix (1-32 lowercase letters, digits, or hyphens).", defaultText: "bead"}},
	},
	"configure": {path: "configure", usage: "wbd configure", summary: "Write Viewer Hub configuration for the existing store."},
	"register":  {path: "register", usage: "wbd register", summary: "Register the current repository context."},
	"context":   {path: "context", usage: "wbd context", summary: "Print the current repository context."},
	"create": {
		path: "create", usage: "wbd create <title> [options]", summary: "Create an issue; omitted targeting uses the current repository context.",
		options: []optionSpec{
			{name: "--description", value: "<text>", description: "Issue description."},
			{name: "--type", value: "<type>", description: "bug|feature|task|epic|chore|decision|todo.", defaultText: "task"},
			{name: "--priority", value: "<0-4|P0-P4>", description: "Priority, where 0 is highest.", defaultText: "2"},
			{name: "--assignee", value: "<identity>", description: "Explicit assignee; never inferred from owner, creator, or environment."},
			{name: "--labels", value: "<label,...>", description: "Ordinary labels; repeatable; ctx: labels are wrapper-owned."},
			{name: "--context", value: "<ctx-id>", description: "Complete explicit target set; repeat for todo or epic."},
			{name: "--contextless", description: "Create a contextless todo."},
			{name: "--from-todo", value: "<todo-id>", description: "Create concrete work with a discovered-from edge."},
			{name: "--json", description: "Emit JSON."},
		},
		examples: []string{
			`wbd create "Implement refresh" --type task --assignee agent-7 --json`,
			`wbd create "Investigate refresh" --type todo --contextless --json`,
		},
	},
	"new": {
		path: "new", usage: "wbd new <title> [options]", summary: "Alias of create.",
	},
	"replace": {
		path: "replace", usage: "wbd replace <original-id> (--context <ctx-id>...|--contextless) [options]", summary: "Create a correctly targeted replacement and close the original.",
		options: []optionSpec{
			{name: "--context", value: "<ctx-id>", description: "Complete replacement target set; repeatable."},
			{name: "--contextless", description: "Target a contextless todo."},
			{name: "--title", value: "<text>", description: "Replacement title; defaults to the original."},
			{name: "--description", value: "<text>", description: "Replacement description; defaults to the original."},
			{name: "--type", value: "<type>", description: "Must equal the original issue type."},
			{name: "--priority", value: "<0-4|P0-P4>", description: "Replacement priority; defaults to the original."},
			{name: "--json", description: "Emit JSON."},
		},
	},
	"compatibility": {
		path: "compatibility", usage: "wbd compatibility --json", summary: "Report Hub policy compatibility findings.",
		options: []optionSpec{{name: "--json", description: "Required; emit JSON."}},
	},
	"list": {
		path: "list", usage: "wbd list [options]", summary: "List current-context issues by default, or all Hub contexts explicitly.",
		options: []optionSpec{
			{name: "--status", value: "<status,...>", description: "open|in_progress|blocked|deferred|closed."},
			{name: "--type", value: "<type>", description: "bug|feature|task|epic|chore|decision|todo."},
			{name: "--priority", value: "<0-4|P0-P4>", description: "Exact priority."},
			{name: "--label", value: "<label>", description: "Require a label; repeatable."},
			{name: "--limit", value: "<1-1000>", description: "Maximum results.", defaultText: "underlying client default"},
			{name: "--ready", description: "Only actionable issues with no active blockers."},
			{name: "--all-contexts", description: "Do not add the current-context filter."},
			{name: "--json", description: "Emit JSON."},
		},
		examples: []string{`wbd list --ready --limit 20 --json`, `wbd list --all-contexts --status open,in_progress --json`},
	},
	"show": {
		path: "show", usage: "wbd show <id> [--json]", summary: "Show one issue without changing context registration.",
		options: []optionSpec{{name: "--json", description: "Emit JSON."}},
	},
	"update": {
		path: "update", usage: "wbd update <id> <mutation> [--json]", summary: "Update explicit fields; omitted fields, including assignee, are preserved.",
		options: []optionSpec{
			{name: "--title", value: "<text>", description: "New title."},
			{name: "--description", value: "<text>", description: "New description."},
			{name: "--priority", value: "<0-4|P0-P4>", description: "New priority."},
			{name: "--status", value: "<status>", description: "open|in_progress|blocked|deferred; use close for closed."},
			{name: "--assignee", value: "<identity>", description: `Set explicitly; pass --assignee "" to clear.`, allowEmpty: true},
			{name: "--add-label", value: "<label,...>", description: "Add ordinary labels; repeatable; ctx: labels are wrapper-owned."},
			{name: "--json", description: "Emit JSON."},
		},
		examples: []string{`wbd update <id> --status in_progress --assignee agent-7 --json`, `wbd update <id> --assignee "" --json`},
	},
	"dep": {
		path: "dep", usage: "wbd dep add|remove <issue-id> <depends-on-id> [options]", summary: "Manage dependency edges.",
	},
	"dep add": {
		path: "dep add", usage: "wbd dep add <issue-id> <depends-on-id> [options]", summary: "Add a dependency edge.",
		options: []optionSpec{
			{name: "--type", value: "<type>", description: "blocks|tracks|related|parent-child|discovered-from|until|caused-by|validates|relates-to.", defaultText: "blocks"},
			{name: "--json", description: "Emit JSON."},
		},
	},
	"dep remove": {
		path: "dep remove", usage: "wbd dep remove <issue-id> <depends-on-id> [--json]", summary: "Remove an ordinary dependency edge.",
		options: []optionSpec{{name: "--json", description: "Emit JSON."}},
	},
	"close": {
		path: "close", usage: "wbd close <id> [--reason <text>] [--json]", summary: "Close one issue.",
		options: []optionSpec{{name: "--reason", value: "<text>", description: "Closure reason."}, {name: "--json", description: "Emit JSON."}},
	},
	"reopen": {
		path: "reopen", usage: "wbd reopen <id> [--reason <text>] [--json]", summary: "Reopen one issue unless supersession policy forbids it.",
		options: []optionSpec{{name: "--reason", value: "<text>", description: "Reopen reason."}, {name: "--json", description: "Emit JSON."}},
	},
	"comments": {
		path: "comments", usage: "wbd comments add <issue-id> (<text...> | --file <path>)", summary: "Manage comments on authoritative Hub issues.",
	},
	"comments add": {
		path: "comments add", usage: "wbd comments add <issue-id> (<text...> | --file <path>) [options]", summary: "Add a comment to an authoritative Hub issue.",
		options: []optionSpec{
			{name: "--author", value: "<identity>", description: "Explicit comment author."},
			{name: "--file", value: "<path>", description: "Read comment text from a file instead of positional text."},
			{name: "--json", description: "Emit JSON."},
		},
	},
	"link": {path: "link", usage: "wbd link <bead-id> [commit]", summary: "Correlate current-context concrete work with a commit."},
	"unlink": {
		path: "unlink", usage: "wbd unlink <bead-id> <full-commit-sha>", summary: "Remove one exact current-context commit correlation.",
		examples: []string{`wbd unlink <id> 0123456789abcdef0123456789abcdef01234567`},
	},
}

func init() {
	// Keep aliases byte-for-byte aligned with their canonical parser contract.
	newSpec := commandSpecs["new"]
	newSpec.options = commandSpecs["create"].options
	newSpec.examples = commandSpecs["create"].examples
	commandSpecs["new"] = newSpec
}

func supportedCommands() string {
	commands := make([]string, 0, len(commandOrder))
	for _, name := range commandOrder {
		if strings.Contains(name, " ") {
			continue
		}
		commands = append(commands, name)
	}
	return "supported commands: " + strings.Join(commands, ", ")
}

func specFor(path string) (commandSpec, bool) {
	spec, ok := commandSpecs[path]
	return spec, ok
}

func usageFor(path string) string {
	if spec, ok := specFor(path); ok {
		return "usage: " + spec.usage
	}
	return supportedCommands()
}

type request struct {
	command          string
	subcommand       string
	args             []string
	positionals      []string
	json             bool
	allContexts      bool
	prefix           string
	contexts         []string
	contextless      bool
	fromTodo         string
	commentAuthor    string
	commentFile      string
	commentSeparator bool
}

func commandName(arguments []string) (string, error) {
	if len(arguments) > 0 && arguments[0] == "--json" {
		arguments = arguments[1:]
	}
	if len(arguments) == 0 {
		return "", errors.New(supportedCommands())
	}
	if strings.HasPrefix(arguments[0], "-") {
		return "", fmt.Errorf("unsupported global option: %s", arguments[0])
	}
	return arguments[0], nil
}

func parse(arguments []string) (request, error) {
	var result request
	if len(arguments) > 0 && arguments[0] == "--json" {
		result.json = true
		arguments = arguments[1:]
	}
	if len(arguments) == 0 {
		return result, errors.New(supportedCommands())
	}
	result.command = arguments[0]
	if strings.HasPrefix(result.command, "-") {
		return result, fmt.Errorf("unsupported global option: %s", result.command)
	}
	arguments = arguments[1:]

	if result.command == "bootstrap" {
		if result.json || len(arguments) != 0 && (len(arguments) != 2 || arguments[0] != "--prefix") {
			return result, errors.New(usageFor("bootstrap"))
		}
		result.prefix = "bead"
		if len(arguments) == 2 {
			result.prefix = arguments[1]
		}
		if err := validatePrefix(result.prefix); err != nil {
			return result, err
		}
		return result, nil
	}

	switch result.command {
	case "context", "configure", "register":
		if result.json || len(arguments) != 0 {
			return result, errors.New(usageFor(result.command))
		}
	case "create", "new":
		return parseCreate(result, arguments)
	case "replace":
		return parseReplace(result, arguments)
	case "compatibility":
		if result.json && len(arguments) == 0 {
			return result, nil
		}
		if len(arguments) == 1 && arguments[0] == "--json" {
			result.json = true
			return result, nil
		}
		return result, errors.New(usageFor("compatibility"))
	case "list":
		return parseList(result, arguments)
	case "show":
		return parseShow(result, arguments)
	case "update":
		return parseUpdate(result, arguments)
	case "dep":
		return parseDep(result, arguments)
	case "close", "reopen":
		return parseClose(result, arguments)
	case "comments":
		if len(arguments) == 0 || arguments[0] != "add" {
			return result, errors.New(usageFor("comments"))
		}
		result.subcommand = arguments[0]
		return parseCommentsAdd(result, arguments[1:])
	case "link":
		if result.json || len(arguments) < 1 || len(arguments) > 2 {
			return result, errors.New(usageFor("link"))
		}
		if err := safeID("link", arguments[0]); err != nil {
			return result, err
		}
		if len(arguments) == 2 {
			if err := safeValue("commit", arguments[1]); err != nil {
				return result, err
			}
			if strings.HasPrefix(arguments[1], "-") {
				return result, fmt.Errorf("invalid commit: %s", arguments[1])
			}
		}
		result.positionals = arguments
	case "unlink":
		if result.json || len(arguments) != 2 {
			return result, errors.New(usageFor("unlink"))
		}
		if err := safeID("unlink", arguments[0]); err != nil {
			return result, err
		}
		if err := safeValue("commit", arguments[1]); err != nil {
			return result, err
		}
		if !isFullCommitSHA(arguments[1]) {
			return result, errors.New("unlink requires a full 40- or 64-character hexadecimal commit SHA")
		}
		result.positionals = arguments
	case "init":
		return result, errors.New("direct init is disabled; run 'wbd bootstrap'")
	default:
		return result, errors.New(supportedCommands())
	}
	return result, nil
}

func parseCommentsAdd(result request, arguments []string) (request, error) {
	seen := make(map[string]bool)
	separator := false
	for len(arguments) > 0 {
		argument := arguments[0]
		arguments = arguments[1:]
		if !separator && argument == "--" {
			separator = true
			result.commentSeparator = true
			continue
		}
		if !separator && argument == "--json" {
			if err := setJSON(&result); err != nil {
				return result, err
			}
			continue
		}
		if !separator {
			flag, value, consumed, matched, err := optionValueFor("comments add", argument, arguments)
			if err != nil {
				return result, err
			}
			if matched {
				arguments = arguments[consumed:]
				if err := markSeen(seen, flag); err != nil {
					return result, err
				}
				switch flag {
				case "--author":
					if err := validateCommentAuthor(value); err != nil {
						return result, err
					}
					result.commentAuthor = value
				case "--file":
					result.commentFile = value
				}
				continue
			}
		}
		if strings.HasPrefix(argument, "-") {
			if !separator {
				return result, fmt.Errorf("unsupported option for comments add: %s", argument)
			}
		}
		if len(result.positionals) == 0 {
			if err := safeID("comments add", argument); err != nil {
				return result, err
			}
		} else if err := validateCommentBody(argument); err != nil {
			return result, err
		}
		result.positionals = append(result.positionals, argument)
	}
	if result.commentFile != "" && len(result.positionals) != 1 || result.commentFile == "" && len(result.positionals) < 2 {
		return result, errors.New(usageFor("comments add"))
	}
	return result, nil
}

func isFullCommitSHA(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				if character < 'A' || character > 'F' {
					return false
				}
			}
		}
	}
	return true
}

func parseCreate(result request, arguments []string) (request, error) {
	seen := make(map[string]bool)
	for len(arguments) > 0 {
		argument := arguments[0]
		arguments = arguments[1:]
		if argument == "--json" {
			if err := setJSON(&result); err != nil {
				return result, err
			}
			continue
		}
		if argument == "--contextless" {
			if err := markSeen(seen, argument); err != nil {
				return result, err
			}
			result.contextless = true
			continue
		}
		flag, value, consumed, matched, err := optionValueFor(result.command, argument, arguments)
		if err != nil {
			return result, err
		}
		if matched {
			arguments = arguments[consumed:]
			if flag == "--context" {
				result.contexts = append(result.contexts, value)
				continue
			}
			if flag == "--from-todo" {
				if err := markSeen(seen, flag); err != nil {
					return result, err
				}
				if err := safeID("from-todo", value); err != nil {
					return result, err
				}
				result.fromTodo = value
				continue
			}
			if err := validateCreateOption(flag, value, seen); err != nil {
				return result, err
			}
			result.args = append(result.args, flag, value)
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return result, fmt.Errorf("unsupported option for %s: %s", result.command, argument)
		}
		if err := safeValue("title", argument); err != nil {
			return result, err
		}
		result.positionals = append(result.positionals, argument)
	}
	if len(result.positionals) != 1 {
		return result, errors.New(usageFor(result.command))
	}
	if result.contextless && len(result.contexts) > 0 {
		return result, errors.New("--context and --contextless are mutually exclusive")
	}
	return result, nil
}

func parseReplace(result request, arguments []string) (request, error) {
	seen := make(map[string]bool)
	for len(arguments) > 0 {
		argument := arguments[0]
		arguments = arguments[1:]
		if argument == "--json" {
			if err := setJSON(&result); err != nil {
				return result, err
			}
			continue
		}
		if argument == "--contextless" {
			if err := markSeen(seen, argument); err != nil {
				return result, err
			}
			result.contextless = true
			continue
		}
		flag, value, consumed, matched, err := optionValueFor("replace", argument, arguments)
		if err != nil {
			return result, err
		}
		if matched {
			arguments = arguments[consumed:]
			if flag == "--context" {
				result.contexts = append(result.contexts, value)
				continue
			}
			if err := markSeen(seen, flag); err != nil {
				return result, err
			}
			switch flag {
			case "--type":
				err = validateType(value)
			case "--priority":
				err = validatePriority(value)
			}
			if err != nil {
				return result, err
			}
			result.args = append(result.args, flag, value)
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return result, fmt.Errorf("unsupported option for replace: %s", argument)
		}
		if err := safeID("replace", argument); err != nil {
			return result, err
		}
		result.positionals = append(result.positionals, argument)
	}
	if len(result.positionals) != 1 || len(result.contexts) == 0 && !result.contextless {
		return result, errors.New(usageFor("replace"))
	}
	if result.contextless && len(result.contexts) > 0 {
		return result, errors.New("--context and --contextless are mutually exclusive")
	}
	return result, nil
}

func validateCreateOption(flag, value string, seen map[string]bool) error {
	if flag != "--labels" {
		if err := markSeen(seen, flag); err != nil {
			return err
		}
	}
	switch flag {
	case "--type":
		return validateType(value)
	case "--priority":
		return validatePriority(value)
	case "--labels":
		return validateLabels(value, true)
	case "--assignee":
		return validateAssignee(value)
	}
	return nil
}

func parseList(result request, arguments []string) (request, error) {
	seen := make(map[string]bool)
	for len(arguments) > 0 {
		argument := arguments[0]
		arguments = arguments[1:]
		switch argument {
		case "--json":
			if err := setJSON(&result); err != nil {
				return result, err
			}
			continue
		case "--all-contexts":
			if err := markSeen(seen, argument); err != nil {
				return result, err
			}
			result.allContexts = true
			continue
		case "--ready":
			if err := markSeen(seen, argument); err != nil {
				return result, err
			}
			result.args = append(result.args, argument)
			continue
		}
		flag, value, consumed, matched, err := optionValueFor("list", argument, arguments)
		if err != nil {
			return result, err
		}
		if matched {
			arguments = arguments[consumed:]
			if flag != "--label" {
				if err := markSeen(seen, flag); err != nil {
					return result, err
				}
			}
			switch flag {
			case "--status":
				err = validateStatuses(value)
			case "--type":
				err = validateType(value)
			case "--priority":
				err = validatePriority(value)
			case "--label":
				err = validateLabels(value, false)
			case "--limit":
				err = validateLimit(value)
			}
			if err != nil {
				return result, err
			}
			result.args = append(result.args, flag, value)
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return result, fmt.Errorf("unsupported option for list: %s", argument)
		}
		return result, fmt.Errorf("list does not accept positional arguments: %s", argument)
	}
	return result, nil
}

func parseShow(result request, arguments []string) (request, error) {
	for _, argument := range arguments {
		if argument == "--json" {
			if err := setJSON(&result); err != nil {
				return result, err
			}
		} else if strings.HasPrefix(argument, "-") {
			return result, fmt.Errorf("unsupported option for show: %s", argument)
		} else if err := safeID("show", argument); err != nil {
			return result, err
		} else {
			result.positionals = append(result.positionals, argument)
		}
	}
	if len(result.positionals) != 1 {
		return result, errors.New(usageFor("show"))
	}
	return result, nil
}

func parseUpdate(result request, arguments []string) (request, error) {
	seen := make(map[string]bool)
	mutations := 0
	for len(arguments) > 0 {
		argument := arguments[0]
		arguments = arguments[1:]
		if argument == "--json" {
			if err := setJSON(&result); err != nil {
				return result, err
			}
			continue
		}
		flag, value, consumed, matched, err := optionValueFor("update", argument, arguments)
		if err != nil {
			return result, err
		}
		if matched {
			arguments = arguments[consumed:]
			if flag != "--add-label" {
				if err := markSeen(seen, flag); err != nil {
					return result, err
				}
			}
			switch flag {
			case "--priority":
				err = validatePriority(value)
			case "--status":
				if !oneOf(value, "open", "in_progress", "blocked", "deferred") {
					err = fmt.Errorf("invalid update status: %s", value)
				}
			case "--add-label":
				err = validateLabels(value, true)
			case "--assignee":
				err = validateAssignee(value)
			}
			if err != nil {
				return result, err
			}
			result.args = append(result.args, flag, value)
			mutations++
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return result, fmt.Errorf("unsupported option for update: %s", argument)
		}
		if err := safeID("update", argument); err != nil {
			return result, err
		}
		result.positionals = append(result.positionals, argument)
	}
	if len(result.positionals) != 1 || mutations == 0 {
		return result, errors.New(usageFor("update"))
	}
	return result, nil
}

func parseDep(result request, arguments []string) (request, error) {
	if len(arguments) == 0 || !oneOf(arguments[0], "add", "remove") {
		return result, errors.New(usageFor("dep"))
	}
	result.subcommand = arguments[0]
	arguments = arguments[1:]
	seen := make(map[string]bool)
	for len(arguments) > 0 {
		argument := arguments[0]
		arguments = arguments[1:]
		if argument == "--json" {
			if err := setJSON(&result); err != nil {
				return result, err
			}
			continue
		}
		flag, value, consumed, matched, err := optionValueFor("dep "+result.subcommand, argument, arguments)
		if err != nil {
			return result, err
		}
		if matched {
			if result.subcommand != "add" {
				return result, errors.New("--type is supported only for dep add")
			}
			arguments = arguments[consumed:]
			if err := markSeen(seen, flag); err != nil {
				return result, err
			}
			if !oneOf(value, "blocks", "tracks", "related", "parent-child", "discovered-from", "until", "caused-by", "validates", "relates-to") {
				return result, fmt.Errorf("invalid dependency type: %s", value)
			}
			result.args = append(result.args, flag, value)
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return result, fmt.Errorf("unsupported option for dep %s: %s", result.subcommand, argument)
		}
		if err := safeID("dep "+result.subcommand, argument); err != nil {
			return result, err
		}
		result.positionals = append(result.positionals, argument)
	}
	if len(result.positionals) != 2 {
		return result, errors.New(usageFor("dep " + result.subcommand))
	}
	return result, nil
}

func parseClose(result request, arguments []string) (request, error) {
	seen := make(map[string]bool)
	for len(arguments) > 0 {
		argument := arguments[0]
		arguments = arguments[1:]
		if argument == "--json" {
			if err := setJSON(&result); err != nil {
				return result, err
			}
			continue
		}
		flag, value, consumed, matched, err := optionValueFor(result.command, argument, arguments)
		if err != nil {
			return result, err
		}
		if matched {
			arguments = arguments[consumed:]
			if err := markSeen(seen, flag); err != nil {
				return result, err
			}
			result.args = append(result.args, flag, value)
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return result, fmt.Errorf("unsupported option for %s: %s", result.command, argument)
		}
		if err := safeID(result.command, argument); err != nil {
			return result, err
		}
		result.positionals = append(result.positionals, argument)
	}
	if len(result.positionals) != 1 {
		return result, errors.New(usageFor(result.command))
	}
	return result, nil
}

func optionValueFor(path, argument string, remaining []string) (string, string, int, bool, error) {
	spec, ok := specFor(path)
	if !ok {
		return "", "", 0, false, nil
	}
	for _, option := range spec.options {
		if option.value == "" {
			continue
		}
		if argument == option.name {
			if len(remaining) == 0 {
				return "", "", 0, true, fmt.Errorf("missing value for %s", option.name)
			}
			if err := safeOptionValue(option, remaining[0]); err != nil {
				return "", "", 0, true, err
			}
			return option.name, remaining[0], 1, true, nil
		}
		if strings.HasPrefix(argument, option.name+"=") {
			value := strings.TrimPrefix(argument, option.name+"=")
			if err := safeOptionValue(option, value); err != nil {
				return "", "", 0, true, err
			}
			return option.name, value, 0, true, nil
		}
	}
	return "", "", 0, false, nil
}

func safeOptionValue(option optionSpec, value string) error {
	if value == "" && option.allowEmpty {
		return nil
	}
	return safeValue(option.name, value)
}

func validateAssignee(value string) error {
	return validateIdentity("assignee", value)
}

func validateCommentAuthor(value string) error {
	return validateIdentity("author", value)
}

func validateIdentity(name, value string) error {
	if value == "" {
		return nil
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must contain non-whitespace characters", name)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("invalid control character in %s", name)
		}
	}
	return nil
}

func safeValue(name, value string) error {
	if value == "" {
		return fmt.Errorf("missing value for %s", name)
	}
	if strings.ContainsAny(value, "\n\r\t") {
		return fmt.Errorf("invalid control character in %s", name)
	}
	return nil
}

func validateCommentBody(value string) error {
	if value == "" {
		return errors.New("missing value for comments add")
	}
	for _, character := range value {
		if unicode.IsControl(character) && character != '\n' && character != '\r' && character != '\t' {
			return errors.New("invalid control character in comments add")
		}
	}
	return nil
}

func safeID(name, value string) error {
	if err := safeValue(name, value); err != nil {
		return err
	}
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("invalid ID for %s: %s", name, value)
	}
	return nil
}

func setJSON(result *request) error {
	if result.json {
		return errors.New("duplicate option: --json")
	}
	result.json = true
	return nil
}

func markSeen(seen map[string]bool, option string) error {
	if seen[option] {
		return fmt.Errorf("duplicate option: %s", option)
	}
	seen[option] = true
	return nil
}

func validatePriority(value string) error {
	if oneOf(value, "0", "1", "2", "3", "4", "P0", "P1", "P2", "P3", "P4") {
		return nil
	}
	return fmt.Errorf("invalid priority: %s", value)
}

func validateType(value string) error {
	if oneOf(value, "bug", "feature", "task", "epic", "chore", "decision", "todo") {
		return nil
	}
	return fmt.Errorf("invalid issue type: %s", value)
}

func validateStatuses(value string) error {
	values, err := commaValues(value, "status")
	if err != nil {
		return err
	}
	for _, status := range values {
		if !oneOf(status, "open", "in_progress", "blocked", "deferred", "closed") {
			return fmt.Errorf("invalid status: %s", status)
		}
	}
	return nil
}

func validateLabels(value string, mutation bool) error {
	values, err := commaValues(value, "label")
	if err != nil {
		return err
	}
	for _, label := range values {
		if err := safeValue("label", label); err != nil {
			return err
		}
		if mutation {
			if strings.Contains(label, `"`) {
				return errors.New("quoted labels are unsupported")
			}
			checked := strings.TrimLeft(label, " ")
			if checked == "" {
				return errors.New("label list contains an empty value")
			}
			if strings.HasPrefix(checked, "ctx:") {
				return errors.New("ctx: labels are wrapper-owned")
			}
		}
	}
	return nil
}

func commaValues(value, name string) ([]string, error) {
	if strings.HasPrefix(value, ",") || strings.HasSuffix(value, ",") || strings.Contains(value, ",,") {
		return nil, fmt.Errorf("%s list contains an empty value", name)
	}
	values := strings.Split(value, ",")
	if len(values) == 0 || value == "" {
		return nil, fmt.Errorf("%s list must not be empty", name)
	}
	return values, nil
}

func validateLimit(value string) error {
	if len(value) > 4 || value == "" {
		return fmt.Errorf("invalid limit: %s", value)
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return fmt.Errorf("invalid limit: %s", value)
		}
	}
	limit, _ := strconv.Atoi(value)
	if limit < 1 || limit > 1000 {
		return errors.New("limit must be between 1 and 1000")
	}
	return nil
}

func validatePrefix(prefix string) error {
	valid := len(prefix) >= 1 && len(prefix) <= 32 && prefix[0] >= 'a' && prefix[0] <= 'z' && !strings.Contains(prefix, "--")
	for index, character := range prefix {
		if character < 'a' || character > 'z' {
			if character < '0' || character > '9' {
				if character != '-' {
					valid = false
				}
			}
		}
		if index == len(prefix)-1 && character == '-' {
			valid = false
		}
	}
	if !valid {
		if strings.Contains(prefix, "--") {
			return errors.New("prefix must not contain consecutive hyphens")
		}
		return errors.New("prefix must be 1-32 lowercase ASCII letters, digits, or hyphens, start with a letter, and end with a letter or digit")
	}
	return nil
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}
