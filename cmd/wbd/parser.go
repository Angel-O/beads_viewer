package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
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
	"compatibility", "list", "show", "update", "claim", "unclaim", "dep", "dep add", "dep remove",
	"close", "reopen", "comments", "comments add", "comments edit", "comments delete", "link", "unlink",
	"scope", "scope create", "scope list", "scope show", "scope active", "scope activate", "scope deactivate", "scope add", "scope remove", "scope move",
	"backlog", "backlog list", "migrate",
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
			{name: "--labels", value: "<label,...>", description: "Ordinary labels; repeatable; ctx: labels are wrapper-owned."},
			{name: "--context", value: "<ctx-id>", description: "Complete explicit target set; repeat for todo or epic."},
			{name: "--contextless", description: "Create a contextless todo."},
			{name: "--from-todo", value: "<todo-id>", description: "Create concrete work with a discovered-from edge."},
			{name: "--json", description: "Emit JSON."},
		},
		examples: []string{
			`wbd create "Implement refresh" --type task --json`,
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
			{name: "--paginate", description: "Use the bounded keyset-pagination JSON contract; requires --limit."},
			{name: "--cursor", value: "<token>", description: "Opaque cursor returned by a previous paginated request."},
			{name: "--sort", value: "<field:desc>", description: "Deterministic order: created_at:desc|updated_at:desc|closed_at:desc.", defaultText: "updated_at:desc for structured JSON lists"},
			{name: "--created-after", value: "<RFC3339>", description: "Only created_at strictly after this instant."},
			{name: "--updated-after", value: "<RFC3339>", description: "Only updated_at strictly after this instant."},
			{name: "--closed-after", value: "<RFC3339>", description: "Only closed_at strictly after this instant."},
			{name: "--after-created-at", value: "<RFC3339>", description: "Alias for --created-after."},
			{name: "--after-updated-at", value: "<RFC3339>", description: "Alias for --updated-after."},
			{name: "--after-closed-at", value: "<RFC3339>", description: "Alias for --closed-after."},
			{name: "--brief", description: "Use the fixed compact issue projection."},
			{name: "--ready", description: "Only actionable issues with no active blockers."},
			{name: "--all-contexts", description: "Do not add the current-context filter."},
			{name: "--json", description: "Emit JSON."},
		},
		examples: []string{`wbd list --ready --limit 20 --json`, `wbd list --all-contexts --status open,in_progress --json`, `wbd list --paginate --limit 50 --sort updated_at:desc --brief --json`},
	},
	"show": {
		path: "show", usage: "wbd show <id> [options]", summary: "Show one issue without changing context registration; use comments to read its comments.",
		options: []optionSpec{
			{name: "--expand-dependencies", description: "Restore fully expanded dependency and dependent objects; requires --json."},
			{name: "--json", description: "Emit JSON."},
		},
	},
	"update": {
		path: "update", usage: "wbd update <id> <mutation> [--json]", summary: "Update issue fields other than claim ownership.",
		options: []optionSpec{
			{name: "--title", value: "<text>", description: "New title."},
			{name: "--description", value: "<text>", description: "New description."},
			{name: "--priority", value: "<0-4|P0-P4>", description: "New priority."},
			{name: "--status", value: "<status>", description: "open|in_progress|blocked|deferred; use close for closed."},
			{name: "--add-label", value: "<label,...>", description: "Add ordinary labels; repeatable; ctx: labels are wrapper-owned."},
			{name: "--json", description: "Emit JSON."},
		},
		examples: []string{`wbd update <id> --status blocked --json`},
	},
	"claim": {
		path: "claim", usage: "wbd claim <id> [--json]", summary: "Atomically claim one issue for the invoking actor.",
		options:  []optionSpec{{name: "--json", description: "Emit JSON."}},
		examples: []string{`wbd claim <id> --json`},
	},
	"unclaim": {
		path: "unclaim", usage: "wbd unclaim <id> [options]", summary: "Release your claim, or explicitly recover one abandoned claim.",
		options: []optionSpec{
			{name: "--reason", value: "<text>", description: "Reason for releasing the claim."},
			{name: "--force", description: "Force-clear a different actor's abandoned claim; requires this exact issue ID."},
			{name: "--json", description: "Emit JSON."},
		},
		examples: []string{`wbd unclaim <id> --reason "Agent crashed" --json`, `wbd unclaim <id> --force --reason "Abandoned claim" --json`},
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
		path: "comments", usage: "wbd comments <issue-id> --json | wbd comments add|edit|delete ...", summary: "Read or manage comments on authoritative Hub issues.",
	},
	"comments add": {
		path: "comments add", usage: "wbd comments add <issue-id> (<text...> | --file <path>) [options]", summary: "Add a comment to an authoritative Hub issue.",
		options: []optionSpec{
			{name: "--author", value: "<identity>", description: "Explicit comment author."},
			{name: "--file", value: "<path>", description: "Read comment text from a file instead of positional text."},
			{name: "--json", description: "Emit JSON."},
		},
	},
	"comments edit": {
		path: "comments edit", usage: "wbd comments edit <issue-id> <comment-id> (<text...> | --stdin | --file <path>) [options]", summary: "Edit a comment on an authoritative Hub issue.",
		options: []optionSpec{
			{name: "--file", value: "<path>", description: "Read replacement text from a file instead of positional text."},
			{name: "--stdin", description: "Read replacement text from standard input instead of positional text."},
			{name: "--json", description: "Emit JSON."},
		},
	},
	"comments delete": {
		path: "comments delete", usage: "wbd comments delete <issue-id> <comment-id> [options]", summary: "Delete a comment from an authoritative Hub issue.",
		options: []optionSpec{{name: "--json", description: "Emit JSON."}},
	},
	"link": {path: "link", usage: "wbd link <bead-id> [commit]", summary: "Correlate current-context concrete work with a commit."},
	"unlink": {
		path: "unlink", usage: "wbd unlink <bead-id> <full-commit-sha>", summary: "Remove one exact current-context commit correlation.",
		examples: []string{`wbd unlink <id> 0123456789abcdef0123456789abcdef01234567`},
	},
	"migrate": {
		path: "migrate", usage: "wbd migrate (--dry-run|--apply) --json", summary: "Copy the local repository store into the private Hub.",
		options: []optionSpec{
			{name: "--dry-run", description: "Validate and report without writing."},
			{name: "--apply", description: "Back up and apply the migration."},
			{name: "--json", description: "Required; emit JSON."},
		},
	},
	"scope": {path: "scope", usage: "wbd scope <create|list|show|active|activate|deactivate|add|remove|move> [options]", summary: "Manage named backlog scopes through bd."},
	"scope create": {
		path: "scope create", usage: "wbd scope create <id> <name> [--activate] [--json]", summary: "Create a named backlog scope.",
		options: []optionSpec{{name: "--activate", description: "Activate the scope in the same backend call."}, {name: "--json", description: "Emit JSON."}},
	},
	"scope list":       {path: "scope list", usage: "wbd scope list [--json]", summary: "List named backlog scopes.", options: []optionSpec{{name: "--json", description: "Emit JSON."}}},
	"scope show":       {path: "scope show", usage: "wbd scope show <id> [--json]", summary: "Show one backlog scope.", options: []optionSpec{{name: "--json", description: "Emit JSON."}}},
	"scope active":     {path: "scope active", usage: "wbd scope active [--json]", summary: "Show the active backlog scope.", options: []optionSpec{{name: "--json", description: "Emit JSON."}}},
	"scope activate":   {path: "scope activate", usage: "wbd scope activate <id> [--json]", summary: "Activate a backlog scope.", options: []optionSpec{{name: "--json", description: "Emit JSON."}}},
	"scope deactivate": {path: "scope deactivate", usage: "wbd scope deactivate [--json]", summary: "Deactivate the active backlog scope.", options: []optionSpec{{name: "--json", description: "Emit JSON."}}},
	"scope add":        {path: "scope add", usage: "wbd scope add <issue-id>... [--scope <scope-id>] [--json]", summary: "Add issues to a scope; omitted scope uses the active scope.", options: []optionSpec{{name: "--scope", value: "<scope-id>", description: "Target scope; defaults to the active scope."}, {name: "--json", description: "Emit JSON."}}},
	"scope remove":     {path: "scope remove", usage: "wbd scope remove <issue-id>... [--scope <scope-id>] [--json]", summary: "Remove issues from a scope; omitted scope uses the active scope.", options: []optionSpec{{name: "--scope", value: "<scope-id>", description: "Target scope; defaults to the active scope."}, {name: "--json", description: "Emit JSON."}}},
	"scope move":       {path: "scope move", usage: "wbd scope move <issue-id>... [--source-scope <id>] [--target-scope <id>] [--json]", summary: "Move issues between scopes; omitted source or target uses the active scope.", options: []optionSpec{{name: "--source-scope", value: "<id>", description: "Source scope; defaults to the active scope."}, {name: "--target-scope", value: "<id>", description: "Target scope; defaults to the active scope."}, {name: "--json", description: "Emit JSON."}}},
	"backlog":          {path: "backlog", usage: "wbd backlog list [options]", summary: "Read the scoped backlog through bd's JSON surface."},
	"backlog list": {
		path: "backlog list", usage: "wbd backlog list [--limit <n>] [--cursor <token>] [--json]", summary: "List unscoped backlog issues, preserving bd pagination cursors unchanged.",
		options: []optionSpec{{name: "--limit", value: "<1-1000>", description: "Maximum results."}, {name: "--cursor", value: "<token>", description: "Opaque cursor returned by a previous page."}, {name: "--json", description: "Emit JSON."}},
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
	command            string
	subcommand         string
	args               []string
	positionals        []string
	json               bool
	allContexts        bool
	prefix             string
	contexts           []string
	contextless        bool
	fromTodo           string
	force              bool
	commentAuthor      string
	commentFile        string
	commentStdin       bool
	commentSeparator   bool
	listPaginate       bool
	listLimitSet       bool
	listCursor         string
	listSort           string
	listAfterCreated   string
	listAfterUpdated   string
	listAfterClosed    string
	listBrief          bool
	expandDependencies bool
	migrateDryRun      bool
	migrateApply       bool
	scopeSubcommand    string
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
	case "scope":
		return parseScope(result, arguments)
	case "backlog":
		return parseBacklog(result, arguments)
	case "show":
		return parseShow(result, arguments)
	case "update":
		return parseUpdate(result, arguments)
	case "claim":
		return parseClaim(result, arguments)
	case "unclaim":
		return parseUnclaim(result, arguments)
	case "dep":
		return parseDep(result, arguments)
	case "close", "reopen":
		return parseClose(result, arguments)
	case "migrate":
		return parseMigrate(result, arguments)
	case "comments":
		if len(arguments) == 0 {
			return result, errors.New(usageFor("comments"))
		}
		if !oneOf(arguments[0], "add", "edit", "delete") {
			return parseCommentsRead(result, arguments)
		}
		result.subcommand = arguments[0]
		switch result.subcommand {
		case "add":
			return parseCommentsAdd(result, arguments[1:])
		case "edit":
			return parseCommentsEdit(result, arguments[1:])
		default:
			return parseCommentsDelete(result, arguments[1:])
		}
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

func parseMigrate(result request, arguments []string) (request, error) {
	seen := make(map[string]bool)
	for _, argument := range arguments {
		switch argument {
		case "--json":
			if err := setJSON(&result); err != nil {
				return result, err
			}
		case "--dry-run", "--apply":
			if err := markSeen(seen, argument); err != nil {
				return result, err
			}
			if argument == "--dry-run" {
				result.migrateDryRun = true
			} else {
				result.migrateApply = true
			}
		default:
			return result, fmt.Errorf("unsupported option for migrate: %s", argument)
		}
	}
	if !result.json || result.migrateDryRun == result.migrateApply {
		return result, errors.New(usageFor("migrate"))
	}
	return result, nil
}

func parseCommentsRead(result request, arguments []string) (request, error) {
	for _, argument := range arguments {
		if argument == "--json" {
			if err := setJSON(&result); err != nil {
				return result, err
			}
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return result, fmt.Errorf("unsupported option for comments: %s", argument)
		}
		if err := safeID("comments", argument); err != nil {
			return result, err
		}
		result.positionals = append(result.positionals, argument)
	}
	if !result.json || len(result.positionals) != 1 {
		return result, errors.New(usageFor("comments"))
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

func parseCommentsEdit(result request, arguments []string) (request, error) {
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
			if argument == "--stdin" {
				if err := markSeen(seen, argument); err != nil {
					return result, err
				}
				result.commentStdin = true
				continue
			}
			flag, value, consumed, matched, err := optionValueFor("comments edit", argument, arguments)
			if err != nil {
				return result, err
			}
			if matched {
				arguments = arguments[consumed:]
				if err := markSeen(seen, flag); err != nil {
					return result, err
				}
				result.commentFile = value
				continue
			}
		}
		if strings.HasPrefix(argument, "-") && !separator {
			return result, fmt.Errorf("unsupported option for comments edit: %s", argument)
		}
		if len(result.positionals) < 2 {
			if err := safeID("comments edit", argument); err != nil {
				return result, err
			}
		} else if err := validateCommentEditBody(argument); err != nil {
			return result, err
		}
		result.positionals = append(result.positionals, argument)
	}

	if len(result.positionals) < 2 {
		return result, errors.New(usageFor("comments edit"))
	}
	sourceSelected := result.commentFile != "" || result.commentStdin
	if sourceSelected {
		if len(result.positionals) != 2 || result.commentFile != "" && result.commentStdin {
			return result, errors.New(usageFor("comments edit"))
		}
	} else if len(result.positionals) < 3 {
		return result, errors.New(usageFor("comments edit"))
	}
	return result, nil
}

func parseCommentsDelete(result request, arguments []string) (request, error) {
	for len(arguments) > 0 {
		argument := arguments[0]
		arguments = arguments[1:]
		if argument == "--json" {
			if err := setJSON(&result); err != nil {
				return result, err
			}
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return result, fmt.Errorf("unsupported option for comments delete: %s", argument)
		}
		if err := safeID("comments delete", argument); err != nil {
			return result, err
		}
		result.positionals = append(result.positionals, argument)
	}
	if len(result.positionals) != 2 {
		return result, errors.New(usageFor("comments delete"))
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
		case "--paginate":
			if err := markSeen(seen, argument); err != nil {
				return result, err
			}
			result.listPaginate = true
			continue
		case "--brief":
			if err := markSeen(seen, argument); err != nil {
				return result, err
			}
			result.listBrief = true
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
				result.listLimitSet = true
			case "--cursor":
				result.listCursor = value
			case "--sort":
				err = validateListSort(value)
				result.listSort = value
			case "--created-after", "--after-created-at":
				if result.listAfterCreated != "" {
					return result, errors.New("--created-after and --after-created-at cannot be specified together")
				}
				err = validateAfterTimestamp(flag, value)
				result.listAfterCreated = value
			case "--updated-after", "--after-updated-at":
				if result.listAfterUpdated != "" {
					return result, errors.New("--updated-after and --after-updated-at cannot be specified together")
				}
				err = validateAfterTimestamp(flag, value)
				result.listAfterUpdated = value
			case "--closed-after", "--after-closed-at":
				if result.listAfterClosed != "" {
					return result, errors.New("--closed-after and --after-closed-at cannot be specified together")
				}
				err = validateAfterTimestamp(flag, value)
				result.listAfterClosed = value
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
	if result.listPaginate && !result.listLimitSet {
		return result, errors.New("--paginate requires --limit so the page is bounded")
	}
	if result.listCursor != "" && !result.listLimitSet {
		return result, errors.New("--cursor requires --limit so the page is bounded")
	}
	return result, nil
}

func parseScope(result request, arguments []string) (request, error) {
	if len(arguments) == 0 || !oneOf(arguments[0], "create", "list", "show", "active", "activate", "deactivate", "add", "remove", "move") {
		return result, errors.New(usageFor("scope"))
	}
	result.scopeSubcommand = arguments[0]
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
		if argument == "--activate" {
			if result.scopeSubcommand != "create" {
				return result, fmt.Errorf("unsupported option for scope %s: %s", result.scopeSubcommand, argument)
			}
			if err := markSeen(seen, argument); err != nil {
				return result, err
			}
			result.args = append(result.args, argument)
			continue
		}
		flag, value, consumed, matched, err := optionValueFor("scope "+result.scopeSubcommand, argument, arguments)
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
			return result, fmt.Errorf("unsupported option for scope %s: %s", result.scopeSubcommand, argument)
		}
		if err := safeValue("scope", argument); err != nil {
			return result, err
		}
		result.positionals = append(result.positionals, argument)
	}

	wantPositionals := 0
	switch result.scopeSubcommand {
	case "create":
		wantPositionals = 2
	case "show", "activate":
		wantPositionals = 1
	case "deactivate":
		wantPositionals = 0
	case "add", "remove":
		if len(result.positionals) == 0 {
			return result, errors.New(usageFor("scope " + result.scopeSubcommand))
		}
		wantPositionals = len(result.positionals)
	case "move":
		if len(result.positionals) == 0 {
			return result, errors.New(usageFor("scope move"))
		}
		wantPositionals = len(result.positionals)
	}
	if len(result.positionals) != wantPositionals {
		return result, errors.New(usageFor("scope " + result.scopeSubcommand))
	}
	return result, nil
}

func parseBacklog(result request, arguments []string) (request, error) {
	if len(arguments) == 0 || arguments[0] != "list" {
		return result, errors.New(usageFor("backlog"))
	}
	result.scopeSubcommand = "list"
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
		flag, value, consumed, matched, err := optionValueFor("backlog list", argument, arguments)
		if err != nil {
			return result, err
		}
		if matched {
			arguments = arguments[consumed:]
			if err := markSeen(seen, flag); err != nil {
				return result, err
			}
			if flag == "--limit" {
				if err := validateLimit(value); err != nil {
					return result, err
				}
			}
			result.args = append(result.args, flag, value)
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return result, fmt.Errorf("unsupported option for backlog list: %s", argument)
		}
		return result, fmt.Errorf("backlog list does not accept positional arguments: %s", argument)
	}
	if result.json && requestValue(result.args, "--limit", "") == "" {
		return result, errors.New("JSON backlog pages require a positive --limit")
	}
	return result, nil
}

func validateListSort(value string) error {
	parts := strings.Split(value, ":")
	if len(parts) != 2 || parts[1] != "desc" || !oneOf(parts[0], "created_at", "updated_at", "closed_at") {
		return fmt.Errorf("invalid sort %q; use created_at:desc, updated_at:desc, or closed_at:desc", value)
	}
	return nil
}

func validateAfterTimestamp(flag, value string) error {
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		return fmt.Errorf("invalid %s %q; use an RFC3339 timestamp such as 2026-08-27T12:00:00Z", flag, value)
	}
	return nil
}

func parseShow(result request, arguments []string) (request, error) {
	seen := make(map[string]bool)
	for _, argument := range arguments {
		if argument == "--json" {
			if err := setJSON(&result); err != nil {
				return result, err
			}
		} else if argument == "--expand-dependencies" {
			if err := markSeen(seen, argument); err != nil {
				return result, err
			}
			result.expandDependencies = true
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

func parseClaim(result request, arguments []string) (request, error) {
	for _, argument := range arguments {
		if argument == "--json" {
			if err := setJSON(&result); err != nil {
				return result, err
			}
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return result, fmt.Errorf("unsupported option for claim: %s", argument)
		}
		if err := safeID("claim", argument); err != nil {
			return result, err
		}
		result.positionals = append(result.positionals, argument)
	}
	if len(result.positionals) != 1 {
		return result, errors.New(usageFor("claim"))
	}
	return result, nil
}

func parseUnclaim(result request, arguments []string) (request, error) {
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
		if argument == "--force" {
			if err := markSeen(seen, argument); err != nil {
				return result, err
			}
			result.force = true
			continue
		}
		flag, value, consumed, matched, err := optionValueFor("unclaim", argument, arguments)
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
			return result, fmt.Errorf("unsupported option for unclaim: %s", argument)
		}
		if err := safeID("unclaim", argument); err != nil {
			return result, err
		}
		result.positionals = append(result.positionals, argument)
	}
	if len(result.positionals) != 1 {
		return result, errors.New(usageFor("unclaim"))
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

func validateCommentEditBody(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("replacement text must contain non-whitespace characters")
	}
	for _, character := range value {
		if unicode.IsControl(character) && character != '\n' && character != '\r' && character != '\t' {
			return errors.New("invalid control character in comments edit")
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
