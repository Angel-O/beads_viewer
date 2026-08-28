package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
)

var isolatedVariables = map[string]bool{
	"BD_DB": true, "BEADS_DB": true, "BD_GLOBAL": true,
	"BEADS_DOLT_DATA_DIR": true, "BEADS_DOLT_PORT": true,
	"BEADS_DOLT_PROXIED_SERVER": true, "BEADS_DOLT_SERVER_DATABASE": true,
	"BEADS_DOLT_SERVER_HOST": true, "BEADS_DOLT_SERVER_MODE": true,
	"BEADS_DOLT_SERVER_PORT": true, "BEADS_DOLT_SERVER_SOCKET": true,
	"BEADS_DOLT_SHARED_SERVER": true, "BEADS_DIR": true,
}

const customIssueTypesKey = "types.custom"

type app struct {
	paths       hub.Paths
	dir         string
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	jsonFailure bool
}

type bdIssue struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	Status       string       `json:"status"`
	Assignee     string       `json:"assignee"`
	Priority     int          `json:"priority"`
	IssueType    string       `json:"issue_type"`
	Labels       []string     `json:"labels"`
	Dependencies []bdRelation `json:"dependencies"`
	Dependents   []bdRelation `json:"dependents"`
}

type bdComment struct {
	ID        string `json:"id"`
	IssueID   string `json:"issue_id"`
	Author    string `json:"author"`
	CreatedAt string `json:"created_at"`
	Text      string `json:"text"`
}

type bdRelation struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	IssueType      string `json:"issue_type"`
	DependencyType string `json:"dependency_type"`
}

func (i bdIssue) policyState() hub.IssueState {
	return hub.IssueState{ID: i.ID, Kind: i.IssueType, Status: i.Status, Labels: i.Labels}
}

type graphPlan struct {
	Nodes []graphNode `json:"nodes"`
	Edges []graphEdge `json:"edges,omitempty"`
}

type graphNode struct {
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Priority    int      `json:"priority"`
	Labels      []string `json:"labels,omitempty"`
}

type graphEdge struct {
	FromKey string `json:"from_key,omitempty"`
	FromID  string `json:"from_id,omitempty"`
	ToKey   string `json:"to_key,omitempty"`
	ToID    string `json:"to_id,omitempty"`
	Type    string `json:"type"`
}

type compatibilityFinding struct {
	Code    string `json:"code"`
	IssueID string `json:"issue_id"`
	Related string `json:"related_id,omitempty"`
	Value   string `json:"value,omitempty"`
	Message string `json:"message"`
}

func newApp(stdin io.Reader, stdout, stderr io.Writer) (*app, error) {
	paths, err := hub.DefaultPaths()
	if err != nil {
		return nil, err
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolving current directory: %w", err)
	}
	return &app{paths: paths, dir: dir, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

func (a *app) run(arguments []string) int {
	if target, requested, err := helpTarget(arguments); requested {
		if err != nil {
			return a.fail(err)
		}
		a.printHelp(target)
		return 0
	}
	a.jsonFailure = containsArgument(arguments, "--json")
	command, err := commandName(arguments)
	if err != nil {
		return a.fail(err)
	}
	if command == "bootstrap" {
		request, parseErr := parse(arguments)
		if parseErr != nil {
			return a.fail(parseErr)
		}
		return a.bootstrap(request.prefix)
	}
	if info, statErr := os.Stat(a.paths.Store); statErr != nil || !info.IsDir() {
		return a.fail(errors.New("store is missing; run 'wbd bootstrap'"))
	}
	request, err := parse(arguments)
	if err != nil {
		return a.fail(err)
	}
	if !usesDatabaseList(request) {
		if err := need("bd"); err != nil {
			return a.fail(err)
		}
	}

	switch request.command {
	case "context":
		context, contextErr := hub.Context(a.dir)
		if contextErr != nil {
			return a.fail(contextErr)
		}
		fmt.Fprintln(a.stdout, context)
		return 0
	case "configure":
		registration, configureErr := hub.Configure(a.paths, a.dir)
		if configureErr != nil {
			return a.fail(configureErr)
		}
		a.signalRegistration(registration)
		fmt.Fprintln(a.stdout, a.paths.Config)
		return 0
	case "register":
		registration, registerErr := a.register()
		if registerErr != nil {
			return a.fail(registerErr)
		}
		fmt.Fprintf(a.stdout, "%s\t%s\n", registration.Context, registration.Root)
		return 0
	case "create", "new":
		return a.create(request)
	case "replace":
		return a.replace(request)
	case "compatibility":
		return a.compatibility()
	case "list":
		if usesDatabaseList(request) {
			return a.list(request)
		}
		args := appendJSON(nil, request.json)
		args = append(args, "list")
		if !request.allContexts {
			registration, registerErr := a.register()
			if registerErr != nil {
				return a.fail(registerErr)
			}
			args = append(args, "--label", registration.Context)
		}
		args = append(args, request.args...)
		return a.runBD(a.dir, args...)
	case "show":
		args := appendJSON(nil, request.json)
		args = append(args, "show", request.positionals[0])
		return a.runBD(a.dir, args...)
	case "update":
		if requestValue(request.args, "--status", "") != "" {
			issue, issueErr := a.readIssue(request.positionals[0], true)
			if issueErr != nil {
				return a.fail(issueErr)
			}
			if issue.Status == "closed" {
				if policyErr := validateReactivation(issue); policyErr != nil {
					return a.fail(policyErr)
				}
			}
		}
		args := appendJSON(nil, request.json)
		args = append(args, "update", request.positionals[0])
		args = append(args, request.args...)
		return a.runBDMutation(a.dir, args...)
	case "claim":
		args := appendJSON(nil, request.json)
		args = append(args, "update", request.positionals[0], "--claim")
		return a.runBDMutation(a.dir, args...)
	case "unclaim":
		if request.force {
			return a.forceUnclaim(request)
		}
		args := appendJSON(nil, request.json)
		args = append(args, "unclaim", request.positionals[0])
		args = append(args, request.args...)
		return a.runBDMutation(a.dir, args...)
	case "dep":
		if request.subcommand == "add" && requestValue(request.args, "--type", "blocks") == "parent-child" {
			child, childErr := a.readIssue(request.positionals[0], false)
			if childErr != nil {
				return a.fail(childErr)
			}
			parent, parentErr := a.readIssue(request.positionals[1], false)
			if parentErr != nil {
				return a.fail(parentErr)
			}
			if policyErr := hub.ValidateEpicChild(parent.policyState(), child.policyState()); policyErr != nil {
				return a.fail(policyErr)
			}
		}
		if request.subcommand == "remove" {
			source, sourceErr := a.readIssue(request.positionals[0], false)
			if sourceErr != nil {
				return a.fail(sourceErr)
			}
			var target *bdIssue
			for _, dependency := range source.Dependencies {
				if dependency.ID != request.positionals[1] || dependency.DependencyType != "supersedes" && dependency.DependencyType != "discovered-from" {
					continue
				}
				if target == nil {
					issue, targetErr := a.readIssue(request.positionals[1], false)
					if targetErr != nil {
						return a.fail(targetErr)
					}
					target = &issue
				}
				if policyErr := hub.ValidateLifecycleRemoval(source.policyState(), target.policyState(), dependency.DependencyType); policyErr != nil {
					return a.fail(policyErr)
				}
			}
		}
		args := appendJSON(nil, request.json)
		args = append(args, "dep", request.subcommand)
		args = append(args, request.positionals...)
		args = append(args, request.args...)
		return a.runBDMutation(a.dir, args...)
	case "close", "reopen":
		if request.command == "reopen" {
			issue, issueErr := a.readIssue(request.positionals[0], true)
			if issueErr != nil {
				return a.fail(issueErr)
			}
			if policyErr := validateReactivation(issue); policyErr != nil {
				return a.fail(policyErr)
			}
		}
		args := appendJSON(nil, request.json)
		args = append(args, request.command, request.positionals[0])
		args = append(args, request.args...)
		return a.runBDMutation(a.dir, args...)
	case "comments":
		switch request.subcommand {
		case "":
			return a.commentsRead(request)
		case "add":
			return a.commentsAdd(request)
		case "edit":
			return a.commentsEdit(request)
		default:
			return a.commentsDelete(request)
		}
	case "link":
		if err := need("bv"); err != nil {
			return a.fail(err)
		}
		issue, issueErr := a.readIssue(request.positionals[0], false)
		if issueErr != nil {
			return a.fail(issueErr)
		}
		if policyErr := hub.ValidateCorrelationOwner(issue.policyState()); policyErr != nil {
			return a.fail(policyErr)
		}
		registration, registerErr := a.register()
		if registerErr != nil {
			return a.fail(registerErr)
		}
		commit := "HEAD"
		if len(request.positionals) == 2 {
			commit = request.positionals[1]
		}
		output, code := a.runBVCapture("correlate", "add", "--bead", request.positionals[0], "--repo", registration.Context, "--commit", commit, "--hub-config", a.paths.Config)
		committed := code == 0
		if code != 0 {
			var result struct {
				Correlation struct {
					BeadID  string `json:"bead_id"`
					Context string `json:"context"`
					Commit  string `json:"commit"`
				} `json:"correlation"`
				Added      bool   `json:"added"`
				Durability string `json:"durability_error"`
			}
			if err := json.Unmarshal(output, &result); err == nil {
				committed = result.Added && result.Durability != "" &&
					result.Correlation.BeadID == request.positionals[0] &&
					result.Correlation.Context == registration.Context &&
					isFullCommitSHA(result.Correlation.Commit)
			}
		}
		if committed {
			a.signalMutation("link")
		}
		if _, err := a.stdout.Write(output); err != nil {
			return a.fail(fmt.Errorf("writing correlation addition result: %w", err))
		}
		return code
	case "unlink":
		if err := need("bv"); err != nil {
			return a.fail(err)
		}
		issue, issueErr := a.readIssue(request.positionals[0], false)
		if issueErr != nil {
			return a.fail(issueErr)
		}
		if policyErr := hub.ValidateCorrelationOwner(issue.policyState()); policyErr != nil {
			return a.fail(policyErr)
		}
		registration, registerErr := a.register()
		if registerErr != nil {
			return a.fail(registerErr)
		}
		output, code := a.runBVCapture("correlate", "remove", "--bead", request.positionals[0], "--repo", registration.Context, "--commit", request.positionals[1], "--hub-config", a.paths.Config)
		var result struct {
			Correlation struct {
				BeadID  string `json:"bead_id"`
				Context string `json:"context"`
				Commit  string `json:"commit"`
			} `json:"correlation"`
			Removed    bool   `json:"removed"`
			Durability string `json:"durability_error"`
		}
		if err := json.Unmarshal(output, &result); err != nil {
			if code != 0 {
				return code
			}
			return a.fail(fmt.Errorf("decoding correlation removal result: %w", err))
		}
		tupleValid := result.Correlation.BeadID == request.positionals[0] &&
			result.Correlation.Context == registration.Context &&
			isFullCommitSHA(result.Correlation.Commit) &&
			strings.EqualFold(result.Correlation.Commit, request.positionals[1])
		if code == 0 && !tupleValid {
			return a.fail(errors.New("correlation removal returned a different tuple than requested"))
		}
		if result.Removed && tupleValid && (code == 0 || result.Durability != "") {
			a.signalMutation("unlink")
		}
		if _, err := a.stdout.Write(output); err != nil {
			return a.fail(fmt.Errorf("writing correlation removal result: %w", err))
		}
		return code
	default:
		return a.fail(errors.New("internal unsupported command"))
	}
}

func usesDatabaseList(request request) bool {
	return request.listPaginate || request.listCursor != "" || request.listSort != "" ||
		request.listAfterCreated != "" || request.listAfterUpdated != "" ||
		request.listAfterClosed != "" || request.listBrief
}

func (a *app) list(request request) int {
	if !request.json {
		return a.fail(errors.New("database-backed list options require --json"))
	}
	if request.listPaginate && !request.listLimitSet {
		return a.fail(errors.New("--paginate requires --limit so the page is bounded"))
	}

	options := hub.ListOptions{AllContexts: request.allContexts, Limit: 0, Paginate: request.listPaginate || request.listCursor != "", Cursor: request.listCursor, Sort: request.listSort, Brief: request.listBrief}
	if request.listLimitSet {
		value := listArgumentValue(request.args, "--limit")
		limit, err := strconv.Atoi(value)
		if err != nil {
			return a.fail(fmt.Errorf("invalid list limit %q: %w", value, err))
		}
		options.Limit = limit
	}
	for index := 0; index+1 < len(request.args); index++ {
		if request.args[index] == "--status" {
			options.Statuses = strings.Split(request.args[index+1], ",")
			index++
		}
	}
	options.IssueType = listArgumentValue(request.args, "--type")
	priorityValue := listArgumentValue(request.args, "--priority")
	if value := priorityValue; value != "" {
		value = strings.TrimPrefix(value, "P")
		priority, err := strconv.Atoi(value)
		if err != nil {
			return a.fail(fmt.Errorf("invalid list priority %q: %w", value, err))
		}
		options.Priority = &priority
	}
	for index := 0; index+1 < len(request.args); index++ {
		if request.args[index] == "--label" {
			options.Labels = append(options.Labels, strings.Split(request.args[index+1], ",")...)
			index++
		}
	}
	for index, argument := range request.args {
		if argument == "--ready" {
			options.Ready = true
		}
		if index+1 >= len(request.args) {
			continue
		}
		switch argument {
		case "--after-created-at":
			value, err := time.Parse(time.RFC3339, request.args[index+1])
			if err != nil {
				return a.fail(fmt.Errorf("invalid %s: %w", argument, err))
			}
			options.AfterCreatedAt = &value
		case "--after-updated-at":
			value, err := time.Parse(time.RFC3339, request.args[index+1])
			if err != nil {
				return a.fail(fmt.Errorf("invalid %s: %w", argument, err))
			}
			options.AfterUpdatedAt = &value
		case "--after-closed-at":
			value, err := time.Parse(time.RFC3339, request.args[index+1])
			if err != nil {
				return a.fail(fmt.Errorf("invalid %s: %w", argument, err))
			}
			options.AfterClosedAt = &value
		}
	}
	if !request.allContexts {
		registration, err := a.register()
		if err != nil {
			return a.fail(err)
		}
		options.Context = registration.Context
	}
	page, err := hub.ListIssues(a.paths.Store, options)
	if err != nil {
		return a.fail(err)
	}
	if page.Pagination != nil {
		return a.writeJSON(map[string]any{"issues": page.Issues, "pagination": page.Pagination})
	}
	return a.writeJSON(page.Issues)
}

func listArgumentValue(arguments []string, flag string) string {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == flag {
			return arguments[index+1]
		}
	}
	return ""
}

func helpTarget(arguments []string) (string, bool, error) {
	help := false
	filtered := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		if argument == "--help" || argument == "-h" {
			help = true
			continue
		}
		filtered = append(filtered, argument)
	}
	if !help {
		return "", false, nil
	}
	if len(filtered) > 0 && filtered[0] == "--json" {
		filtered = filtered[1:]
	}
	if len(filtered) == 0 {
		return "", true, nil
	}
	if _, ok := specFor(filtered[0]); !ok {
		return "", true, errors.New(supportedCommands())
	}
	path := filtered[0]
	if (path == "dep" || path == "comments") && len(filtered) > 1 {
		candidate := path + " " + filtered[1]
		if _, ok := specFor(candidate); !ok {
			return "", true, errors.New(usageFor(path))
		}
		path = candidate
	}
	return path, true, nil
}

func (a *app) printHelp(path string) {
	if path != "" {
		spec, _ := specFor(path)
		fmt.Fprintf(a.stdout, "Usage: %s\n\n%s\n", spec.usage, spec.summary)
		if len(spec.options) > 0 {
			fmt.Fprintln(a.stdout, "\nOptions:")
			for _, option := range spec.options {
				name := option.name
				if option.value != "" {
					name += " " + option.value
				}
				detail := option.description
				if option.defaultText != "" {
					detail += " (default: " + option.defaultText + ")"
				}
				fmt.Fprintf(a.stdout, "  %-31s %s\n", name, detail)
			}
		}
		fmt.Fprintln(a.stdout, "  -h, --help                      Show this help without opening the Hub store.")
		if len(spec.examples) > 0 {
			fmt.Fprintln(a.stdout, "\nExamples:")
			for _, example := range spec.examples {
				fmt.Fprintln(a.stdout, "  "+example)
			}
		}
		return
	}

	fmt.Fprint(a.stdout, `Usage: wbd <command> [options]

wbd is the safe command boundary for the private Beads Hub.

Choose an issue type:
  todo      Capture something not yet concrete project work. It may be
            contextless or span contexts and cannot own commit correlations.
  epic      Coordinate related project work across one or more contexts.
  task, bug, feature, chore
            Track concrete work in exactly one context.
  decision  Record a decision in the current context.

Creation targeting:
  (omitted)              Use the current repository context.
  --context <ctx-id>     Supply the complete target set; repeat for todo/epic.
  --contextless          Create a todo without repository context.
  --from-todo <todo-id>  Create concrete work discovered from a todo.

Claim ownership:
  wbd claim <id>         Atomically assign the invoking actor and start work.
  wbd unclaim <id>       Release your own claim and return work to open.
  wbd unclaim <id> --force --reason "..."
                         Recover one abandoned claim by exact issue ID.
  create and update never accept --assignee; use claim instead.

Commands:
`)
	for _, name := range commandOrder {
		if strings.Contains(name, " ") {
			continue
		}
		spec := commandSpecs[name]
		fmt.Fprintf(a.stdout, "  %-14s %s\n", name, spec.summary)
	}
	fmt.Fprint(a.stdout, `
Global options:
  --json     Emit JSON where supported; accepted before or after the command.
  -h, --help Show help without requiring a store or invoking child commands.

link resolves a ref and adds a correlation. unlink requires an exact full SHA.
Both correlation commands return JSON; unlink is idempotent when not found.
show reports comment_count and may set comments_omitted=true; use
wbd comments <issue-id> --json for the authoritative Hub comments.
Use --json for other queries and mutations except context.
`)
}

func validateReactivation(issue bdIssue) error {
	for _, dependent := range issue.Dependents {
		if dependent.DependencyType != "supersedes" {
			continue
		}
		if dependent.ID == "" || dependent.IssueType == "" {
			return fmt.Errorf("issue %s has an incomplete incoming supersession relation", issue.ID)
		}
		if hub.ValidateSupersession(hub.IssueState{ID: dependent.ID, Kind: dependent.IssueType}, issue.policyState()) == nil {
			return &hub.PolicyError{Code: hub.PolicyInvalidSupersession, Field: "status", Value: issue.ID, Message: "a superseded issue cannot be routinely reactivated"}
		}
	}
	return nil
}

func (a *app) create(request request) int {
	kind := requestValue(request.args, "--type", "task")
	if kind == "todo" {
		if err := a.requireTodoCapability(); err != nil {
			return a.fail(err)
		}
	}
	if kind == "decision" && (request.contextless || len(request.contexts) > 0) {
		return a.fail(&hub.PolicyError{Code: hub.PolicyInvalidKind, Field: "type", Value: kind, Message: "decision supports default-current creation only"})
	}
	contexts, repositories, err := a.creationTargets(request)
	if err != nil {
		return a.fail(err)
	}
	labels := requestLabels(request.args)
	admitted, err := hub.AdmitIssue(kind, contexts, labels, repositories)
	if err != nil {
		return a.fail(err)
	}
	if request.fromTodo == "" {
		args := appendJSON(nil, request.json)
		args = append(args, request.command)
		if len(admitted.Contexts) > 0 {
			args = append(args, "--labels", strings.Join(admitted.Contexts, ","))
		}
		args = append(args, request.positionals[0])
		args = append(args, request.args...)
		return a.runBDMutation(a.dir, args...)
	}

	todo, err := a.readIssue(request.fromTodo, false)
	if err != nil {
		return a.fail(err)
	}
	proposed := hub.IssueState{ID: "proposed", Kind: admitted.Kind, Labels: admitted.Labels}
	if err := hub.ValidateTodoResult(todo.policyState(), proposed); err != nil {
		return a.fail(err)
	}
	priority, _ := strconv.Atoi(strings.TrimPrefix(requestValue(request.args, "--priority", "2"), "P"))
	plan := graphPlan{
		Nodes: []graphNode{{
			Key: "result", Title: request.positionals[0], Type: admitted.Kind,
			Description: requestValue(request.args, "--description", ""),
			Priority:    priority, Labels: admitted.Labels,
		}},
		Edges: []graphEdge{{FromKey: "result", ToID: todo.ID, Type: "discovered-from"}},
	}
	id, err := a.runGraph(plan, "result")
	if err != nil {
		return a.fail(err)
	}
	a.writeCreated(id, request.json)
	return 0
}

func (a *app) creationTargets(request request) ([]string, map[string]hub.Repository, error) {
	if len(request.contexts) == 0 && !request.contextless {
		registration, err := a.register()
		if err != nil {
			return nil, nil, err
		}
		config, err := hub.Resolve(a.paths.Config)
		if err != nil {
			return nil, nil, err
		}
		return []string{registration.Context}, config.Repositories, nil
	}
	config, err := hub.Resolve(a.paths.Config)
	if err != nil {
		return nil, nil, err
	}
	return request.contexts, config.Repositories, nil
}

func (a *app) replace(request request) int {
	original, err := a.readIssue(request.positionals[0], true)
	if err != nil {
		return a.fail(err)
	}
	if original.Status == "closed" {
		return a.fail(errors.New("cannot replace a closed issue"))
	}
	kind := requestValue(request.args, "--type", original.IssueType)
	if kind != original.IssueType {
		return a.fail(&hub.PolicyError{Code: hub.PolicyInvalidSupersession, Field: "type", Value: kind, Message: "replacement must keep the original issue type"})
	}
	if kind == "decision" {
		return a.fail(&hub.PolicyError{Code: hub.PolicyInvalidSupersession, Field: "type", Value: kind, Message: "decision does not support explicit replacement targeting"})
	}
	config, err := hub.Resolve(a.paths.Config)
	if err != nil {
		return a.fail(err)
	}
	ordinaryLabels := make([]string, 0, len(original.Labels))
	for _, label := range original.Labels {
		if !strings.HasPrefix(label, "ctx:") {
			ordinaryLabels = append(ordinaryLabels, label)
		}
	}
	admitted, err := hub.AdmitIssue(kind, request.contexts, ordinaryLabels, config.Repositories)
	if err != nil {
		return a.fail(err)
	}
	proposed := hub.IssueState{ID: "replacement", Kind: kind, Labels: admitted.Labels}
	if err := hub.ValidateSupersession(proposed, original.policyState()); err != nil {
		return a.fail(err)
	}

	title := requestValue(request.args, "--title", original.Title)
	description := requestValue(request.args, "--description", original.Description)
	priorityText := requestValue(request.args, "--priority", strconv.Itoa(original.Priority))
	priority, _ := strconv.Atoi(strings.TrimPrefix(priorityText, "P"))
	edges := []graphEdge{{FromKey: "replacement", ToID: original.ID, Type: "supersedes"}}
	for _, dependency := range original.Dependencies {
		if isOpenBlocking(dependency) {
			edges = append(edges, graphEdge{FromKey: "replacement", ToID: dependency.ID, Type: "blocks"})
		}
	}
	for _, dependent := range original.Dependents {
		if isOpenBlocking(dependent) {
			edges = append(edges, graphEdge{FromID: dependent.ID, ToKey: "replacement", Type: "blocks"})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		return edges[i].FromID+edges[i].FromKey+edges[i].ToID+edges[i].Type < edges[j].FromID+edges[j].FromKey+edges[j].ToID+edges[j].Type
	})
	plan := graphPlan{Nodes: []graphNode{{Key: "replacement", Title: title, Type: kind, Description: description, Priority: priority, Labels: admitted.Labels}}, Edges: edges}
	replacementID, err := a.runGraph(plan, "replacement")
	if err != nil {
		return a.fail(err)
	}
	_, closeErr := a.runBDCapture(a.dir, "--json", "close", original.ID, "--force", "--reason", "Superseded by "+replacementID)
	if closeErr != nil {
		return a.fail(fmt.Errorf("replacement %s was created but closing original %s failed: %w", replacementID, original.ID, closeErr))
	}
	a.signalMutation("replace")
	a.writeReplacement(original.ID, replacementID, request.json)
	return 0
}

func (a *app) commentsAdd(request request) int {
	issue, err := a.validateCommentIssue(request.positionals[0])
	if err != nil {
		return a.fail(err)
	}

	args := appendJSON(nil, request.json)
	args = append(args, "comments", "add", issue.ID)
	if request.commentSeparator {
		if request.commentAuthor != "" {
			args = append(args, "--author", request.commentAuthor)
		}
		args = append(args, "--")
		args = append(args, request.positionals[1:]...)
	} else {
		if request.commentFile != "" {
			args = append(args, "--file", request.commentFile)
		} else {
			args = append(args, request.positionals[1:]...)
		}
		if request.commentAuthor != "" {
			args = append(args, "--author", request.commentAuthor)
		}
	}
	return a.runBDMutation(a.dir, args...)
}

func (a *app) commentsEdit(request request) int {
	stdinData, err := a.prepareCommentEditInput(request)
	if err != nil {
		return a.fail(err)
	}
	if request.commentStdin {
		a.stdin = bytes.NewReader(nil)
	}
	issue, err := a.validateCommentIssue(request.positionals[0])
	if err != nil {
		return a.fail(err)
	}
	if err := a.validateCommentIdentity(issue.ID, request.positionals[1]); err != nil {
		return a.fail(err)
	}
	if request.commentStdin {
		a.stdin = bytes.NewReader(stdinData)
	}

	args := appendJSON(nil, request.json)
	args = append(args, "comments", "edit", issue.ID, request.positionals[1])
	if request.commentStdin {
		args = append(args, "--stdin")
	} else if request.commentFile != "" {
		args = append(args, "--file", request.commentFile)
	} else {
		if request.commentSeparator {
			args = append(args, "--")
		}
		args = append(args, request.positionals[2:]...)
	}
	return a.runBDMutation(a.dir, args...)
}

func (a *app) commentsDelete(request request) int {
	issue, err := a.validateCommentIssue(request.positionals[0])
	if err != nil {
		return a.fail(err)
	}
	if err := a.validateCommentIdentity(issue.ID, request.positionals[1]); err != nil {
		return a.fail(err)
	}

	args := appendJSON(nil, request.json)
	args = append(args, "comments", "delete", issue.ID, request.positionals[1])
	return a.runBDMutation(a.dir, args...)
}

func (a *app) commentsRead(request request) int {
	issue, err := a.validateCommentIssue(request.positionals[0])
	if err != nil {
		return a.fail(err)
	}
	data, err := a.runBDCapture(a.dir, "--readonly", "--json", "comments", issue.ID)
	if err != nil {
		return a.fail(fmt.Errorf("reading comments for issue %s: %w", issue.ID, err))
	}
	comments, err := decodeComments(data, issue.ID)
	if err != nil {
		return a.fail(fmt.Errorf("decoding comments for issue %s: %w", issue.ID, err))
	}
	return a.writeJSON(comments)
}

func decodeComments(data []byte, issueID string) ([]bdComment, error) {
	if strings.TrimSpace(string(data)) == "" {
		return []bdComment{}, nil
	}
	var comments []bdComment
	if err := json.Unmarshal(data, &comments); err != nil {
		return nil, err
	}
	if comments == nil {
		return []bdComment{}, nil
	}
	for index, comment := range comments {
		if comment.ID == "" {
			return nil, fmt.Errorf("comment at index %d has no stable ID", index)
		}
		if comment.IssueID != issueID {
			return nil, fmt.Errorf("comment %s belongs to issue %s, not %s", comment.ID, comment.IssueID, issueID)
		}
		if comment.CreatedAt == "" {
			return nil, fmt.Errorf("comment %s has no created_at timestamp", comment.ID)
		}
	}
	return comments, nil
}

func (a *app) validateCommentIssue(id string) (bdIssue, error) {
	issue, err := a.readIssue(id, false)
	if err != nil {
		return bdIssue{}, err
	}
	config, err := hub.Resolve(a.paths.Config)
	if err != nil {
		return bdIssue{}, fmt.Errorf("resolving Hub config: %w", err)
	}
	if err := hub.ValidateStoredIssue(issue.policyState(), config.Repositories); err != nil {
		return bdIssue{}, fmt.Errorf("validating issue %s: %w", issue.ID, err)
	}
	return issue, nil
}

func (a *app) validateCommentIdentity(issueID, commentID string) error {
	data, err := a.runBDCapture(a.dir, "--readonly", "--json", "comments", issueID)
	if err != nil {
		return fmt.Errorf("reading comments for issue %s: %w", issueID, err)
	}
	var comments []bdComment
	if err := json.Unmarshal(data, &comments); err != nil {
		return fmt.Errorf("decoding comments for issue %s: %w", issueID, err)
	}
	for _, comment := range comments {
		if comment.ID == commentID && comment.IssueID == issueID {
			return nil
		}
	}
	return fmt.Errorf("comment %s was not found on issue %s", commentID, issueID)
}

func (a *app) prepareCommentEditInput(request request) ([]byte, error) {
	if request.commentFile != "" {
		data, err := os.ReadFile(request.commentFile)
		if err != nil {
			return nil, fmt.Errorf("reading replacement text: %w", err)
		}
		return nil, validateCommentEditBody(string(data))
	}
	if request.commentStdin {
		if a.stdin == nil {
			return nil, errors.New("replacement text stdin is unavailable")
		}
		data, err := io.ReadAll(a.stdin)
		if err != nil {
			return nil, fmt.Errorf("reading replacement text from stdin: %w", err)
		}
		if err := validateCommentEditBody(string(data)); err != nil {
			return nil, err
		}
		return data, nil
	}
	return nil, validateCommentEditBody(strings.Join(request.positionals[2:], " "))
}

func (a *app) compatibility() int {
	config, err := hub.Resolve(a.paths.Config)
	if err != nil {
		return a.fail(err)
	}
	data, err := a.runBDCapture(a.dir, "--json", "list", "--all", "--limit", "0")
	if err != nil {
		return a.fail(fmt.Errorf("reading authoritative issues: %w", err))
	}
	var summaries []bdIssue
	if err := json.Unmarshal(data, &summaries); err != nil {
		return a.fail(fmt.Errorf("decoding authoritative issue list: %w", err))
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].ID < summaries[j].ID })
	issues := make(map[string]bdIssue, len(summaries))
	for _, summary := range summaries {
		issue, readErr := a.readIssue(summary.ID, false)
		if readErr != nil {
			return a.fail(readErr)
		}
		issues[issue.ID] = issue
	}

	findings := make([]compatibilityFinding, 0)
	for _, summary := range summaries {
		issue := issues[summary.ID]
		contexts := hub.Contexts(issue.Labels)
		if _, kindErr := hub.ClassifyKind(issue.IssueType); kindErr != nil {
			findings = appendPolicyFinding(findings, issue.ID, kindErr)
		} else {
			findings = appendPolicyFinding(findings, issue.ID, hub.ValidateCardinality(issue.IssueType, len(contexts)))
		}
		for _, context := range contexts {
			findings = appendPolicyFinding(findings, issue.ID, hub.ValidateRegisteredContexts([]string{context}, config.Repositories))
		}
		for _, relation := range issue.Dependencies {
			target, ok := issues[relation.ID]
			if !ok {
				if relation.DependencyType == "discovered-from" || relation.DependencyType == "parent-child" || relation.DependencyType == "supersedes" {
					findings = append(findings, compatibilityFinding{Code: "malformed_lifecycle_edge", IssueID: issue.ID, Related: relation.ID, Message: "lifecycle relation references a missing issue"})
				}
				continue
			}
			var lifecycleErr error
			switch relation.DependencyType {
			case "discovered-from":
				lifecycleErr = hub.ValidateTodoResult(target.policyState(), issue.policyState())
			case "parent-child":
				if target.IssueType == "epic" {
					lifecycleErr = hub.ValidateEpicChild(target.policyState(), issue.policyState())
				}
			case "supersedes":
				lifecycleErr = hub.ValidateSupersession(issue.policyState(), target.policyState())
				if lifecycleErr == nil && target.Status != "closed" {
					lifecycleErr = errors.New("superseded original is not closed")
				}
			}
			if lifecycleErr != nil {
				findings = append(findings, compatibilityFinding{Code: "malformed_lifecycle_edge", IssueID: issue.ID, Related: target.ID, Message: lifecycleErr.Error()})
			}
		}
	}
	correlationFindings, err := todoCorrelationFindings(config.Ledger, issues)
	if err != nil {
		return a.fail(err)
	}
	findings = append(findings, correlationFindings...)
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i].Code + "\x00" + findings[i].IssueID + "\x00" + findings[i].Related + "\x00" + findings[i].Value
		right := findings[j].Code + "\x00" + findings[j].IssueID + "\x00" + findings[j].Related + "\x00" + findings[j].Value
		return left < right
	})
	return a.writeJSON(map[string]any{"findings": findings})
}

func appendPolicyFinding(findings []compatibilityFinding, issueID string, err error) []compatibilityFinding {
	if err == nil {
		return findings
	}
	var structured *hub.PolicyError
	if !errors.As(err, &structured) {
		return findings
	}
	return append(findings, compatibilityFinding{Code: string(structured.Code), IssueID: issueID, Value: structured.Value, Message: structured.Message})
}

func todoCorrelationFindings(path string, issues map[string]bdIssue) ([]compatibilityFinding, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading authoritative correlation ledger: %w", err)
	}
	defer file.Close()

	var findings []compatibilityFinding
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		var record struct {
			BeadID string `json:"bead_id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil || record.BeadID == "" {
			if err == nil {
				err = errors.New("missing bead_id")
			}
			return nil, fmt.Errorf("reading authoritative correlation ledger line %d: %w", line, err)
		}
		if issue, ok := issues[record.BeadID]; ok && issue.IssueType == "todo" {
			findings = append(findings, compatibilityFinding{Code: "todo_correlation", IssueID: record.BeadID, Message: "todo owns a direct commit correlation"})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading authoritative correlation ledger: %w", err)
	}
	return findings, nil
}

func (a *app) readIssue(id string, includeDependents bool) (bdIssue, error) {
	args := []string{"--json", "show", id}
	if includeDependents {
		args = append(args, "--include-dependents")
	}
	data, err := a.runBDCapture(a.dir, args...)
	if err != nil {
		return bdIssue{}, fmt.Errorf("reading issue %s: %w", id, err)
	}
	var issues []bdIssue
	if err := json.Unmarshal(data, &issues); err != nil {
		return bdIssue{}, fmt.Errorf("decoding issue %s: %w", id, err)
	}
	if len(issues) != 1 || issues[0].IssueType == "" || issues[0].Status == "" {
		return bdIssue{}, fmt.Errorf("issue %s returned an invalid authoritative record", id)
	}
	if issues[0].ID != id {
		return bdIssue{}, fmt.Errorf("issue %s is not the exact canonical issue ID; use %s", id, issues[0].ID)
	}
	return issues[0], nil
}

func (a *app) forceUnclaim(request request) int {
	issue, err := a.readIssue(request.positionals[0], false)
	if err != nil {
		return a.fail(err)
	}

	if issue.Assignee == "" {
		return a.fail(fmt.Errorf("cannot force unclaim %s: issue is unassigned", issue.ID))
	}
	if !oneOf(issue.Status, "open", "in_progress", "blocked", "deferred") {
		return a.fail(fmt.Errorf("cannot force unclaim %s: status %q is not recoverable", issue.ID, issue.Status))
	}

	// The backend unclaim verb only accepts open/in_progress rows. Normalize
	// frozen claimed work without emitting a second JSON result, then delegate
	// the release itself so started_at, leases, and unclaimed events are handled
	// by the backend's native lifecycle operation.
	if issue.Status == "blocked" || issue.Status == "deferred" {
		if _, err := a.runBDCapture(a.dir, "--json", "update", issue.ID, "--status", "open"); err != nil {
			return a.fail(fmt.Errorf("normalizing %s for forced unclaim: %w", issue.ID, err))
		}
		a.signalMutation("forced recovery normalization")
	}

	args := appendJSON(nil, request.json)
	args = append(args, "unclaim", issue.ID)
	args = append(args, request.args...)
	args = append(args, "--force")
	return a.runBDMutation(a.dir, args...)
}

func (a *app) runGraph(plan graphPlan, key string) (string, error) {
	file, err := os.CreateTemp("", "wbd-graph-*.json")
	if err != nil {
		return "", fmt.Errorf("creating graph plan: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("securing graph plan: %w", err)
	}
	if err := json.NewEncoder(file).Encode(plan); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("encoding graph plan: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("closing graph plan: %w", err)
	}
	data, err := a.runBDCapture(a.dir, "--json", "create", "--graph", path)
	if err != nil {
		return "", err
	}
	a.signalMutation("graph mutation")
	var result struct {
		IDs map[string]string `json:"ids"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("decoding graph result: %w", err)
	}
	id := result.IDs[key]
	if id == "" {
		return "", fmt.Errorf("graph result omitted %q issue ID", key)
	}
	return id, nil
}

func (a *app) runBDCapture(directory string, arguments ...string) ([]byte, error) {
	arguments = append([]string{"--db", a.paths.Store}, arguments...)
	command := exec.Command("bd", arguments...)
	command.Dir = directory
	command.Stdin = a.stdin
	command.Env = isolatedEnvironment(a.paths.Store, false)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, errors.New(detail)
	}
	return output, nil
}

func (a *app) signalMutation(operation string) {
	if err := hub.SignalChange(a.paths); err != nil {
		fmt.Fprintf(a.stderr, "wbd: warning: %s succeeded but Viewer notification failed: %v\n", operation, err)
	}
}

func (a *app) writeCreated(id string, jsonOutput bool) {
	if jsonOutput {
		_ = a.writeJSON(map[string]string{"id": id})
		return
	}
	fmt.Fprintf(a.stdout, "Created issue: %s\n", id)
}

func (a *app) writeReplacement(original, replacement string, jsonOutput bool) {
	if jsonOutput {
		_ = a.writeJSON(map[string]string{"original_id": original, "replacement_id": replacement})
		return
	}
	fmt.Fprintf(a.stdout, "Replaced %s with %s\n", original, replacement)
}

func (a *app) writeJSON(value any) int {
	encoder := json.NewEncoder(a.stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return a.fail(fmt.Errorf("encoding JSON output: %w", err))
	}
	return 0
}

func isOpenBlocking(relation bdRelation) bool {
	return (relation.DependencyType == "" || relation.DependencyType == "blocks") && relation.Status != "closed"
}

func requestValue(arguments []string, flag, fallback string) string {
	for index := 0; index+1 < len(arguments); index += 2 {
		if arguments[index] == flag {
			return arguments[index+1]
		}
	}
	return fallback
}

func requestLabels(arguments []string) []string {
	var labels []string
	for index := 0; index+1 < len(arguments); index += 2 {
		if arguments[index] != "--labels" {
			continue
		}
		for _, label := range strings.Split(arguments[index+1], ",") {
			labels = append(labels, strings.TrimSpace(label))
		}
	}
	return labels
}

func containsArgument(arguments []string, wanted string) bool {
	for _, argument := range arguments {
		if argument == wanted {
			return true
		}
	}
	return false
}

func (a *app) register() (hub.Registration, error) {
	registration, err := hub.Register(a.paths, a.dir)
	if err == nil {
		a.signalRegistration(&registration)
	}
	return registration, err
}

func (a *app) signalRegistration(registration *hub.Registration) {
	if registration == nil || !registration.Changed {
		return
	}
	if err := hub.SignalChange(a.paths); err != nil {
		fmt.Fprintf(a.stderr, "wbd: warning: registry mutation succeeded but Viewer notification failed: %v\n", err)
	}
}

func (a *app) bootstrap(prefix string) int {
	if err := need("bd"); err != nil {
		return a.fail(err)
	}
	storeInfo, storeErr := os.Stat(a.paths.Store)
	if storeErr == nil {
		if !storeInfo.IsDir() {
			return a.fail(fmt.Errorf("store path is not a directory: %s", a.paths.Store))
		}
		return a.bootstrapExistingStore()
	}
	if !errors.Is(storeErr, os.ErrNotExist) {
		return a.fail(fmt.Errorf("inspecting store: %w", storeErr))
	}
	if err := need("git"); err != nil {
		return a.fail(err)
	}
	home := os.Getenv("HOME")
	info, err := os.Stat(home)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o222 == 0 {
		return a.fail(fmt.Errorf("HOME is not writable: %s", home))
	}
	parent := filepath.Dir(a.paths.Store)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return a.fail(fmt.Errorf("cannot create store parent: %s", parent))
	}
	info, err = os.Stat(parent)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o222 == 0 {
		return a.fail(fmt.Errorf("store parent is not writable: %s", parent))
	}
	if commandSucceeded(parent, "git", "rev-parse", "--git-dir") {
		return a.fail(fmt.Errorf("store parent must be outside every Git repository: %s", parent))
	}

	steps := []struct {
		bootstrap bool
		args      []string
	}{
		{true, []string{"metrics", "off"}},
		{true, []string{"init", "--prefix", prefix, "--non-interactive", "--skip-hooks", "--skip-agents"}},
		{false, []string{"config", "set", "types.custom", "todo"}},
		{false, []string{"config", "set", "export.auto", "false"}},
		{false, []string{"config", "set", "export.git-add", "false"}},
		{false, []string{"config", "set", "dolt.auto-push", "false"}},
	}
	for _, step := range steps {
		code := a.runBDAt(parent, step.bootstrap, step.args...)
		if code != 0 {
			_ = os.RemoveAll(a.paths.Store)
			return code
		}
	}
	if _, err := hub.EnsureConfig(a.paths); err != nil {
		_ = os.RemoveAll(a.paths.Store)
		return a.fail(err)
	}
	return 0
}

func (a *app) bootstrapExistingStore() int {
	types, err := a.customIssueTypes()
	if err != nil {
		return a.fail(fmt.Errorf("reading existing custom issue types: %w", err))
	}
	if _, err := hub.EnsureConfig(a.paths); err != nil {
		return a.fail(err)
	}
	changed := !containsString(types, "todo")
	if changed {
		types = append(types, "todo")
		if _, err := a.runBDCapture(a.dir, "--json", "config", "set", customIssueTypesKey, strings.Join(types, ",")); err != nil {
			return a.fail(fmt.Errorf("enabling todo issue type: %w", err))
		}
	}
	if changed {
		fmt.Fprintln(a.stdout, "Hub store ready: todo issue type enabled.")
	} else {
		fmt.Fprintln(a.stdout, "Hub store ready: todo issue type already enabled.")
	}
	return 0
}

func (a *app) requireTodoCapability() error {
	types, err := a.customIssueTypes()
	if err != nil {
		return fmt.Errorf("todo issue type capability could not be verified: %w; run 'wbd bootstrap' to enable it", err)
	}
	if !containsString(types, "todo") {
		return errors.New("todo issue type is unavailable in the Hub store; run 'wbd bootstrap' to enable it")
	}
	return nil
}

func (a *app) customIssueTypes() ([]string, error) {
	data, err := a.runBDCapture(a.dir, "--readonly", "--json", "config", "get", customIssueTypesKey)
	if err != nil {
		return nil, err
	}
	var result struct {
		Value *string `json:"value"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decoding custom issue types: %w", err)
	}
	if result.Value == nil {
		return nil, nil
	}
	var types []string
	for _, issueType := range strings.Split(*result.Value, ",") {
		if issueType = strings.TrimSpace(issueType); issueType != "" {
			types = append(types, issueType)
		}
	}
	return types, nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (a *app) runBD(directory string, arguments ...string) int {
	return a.runBDAt(directory, false, arguments...)
}

func (a *app) runBDMutation(directory string, arguments ...string) int {
	code := a.runBD(directory, arguments...)
	if code != 0 {
		return code
	}
	a.signalMutation("mutation")
	return 0
}

func (a *app) runBDAt(directory string, bootstrap bool, arguments ...string) int {
	if !bootstrap {
		arguments = append([]string{"--db", a.paths.Store}, arguments...)
	}
	return a.runChild(directory, "bd", arguments, false)
}

func (a *app) runBVCapture(arguments ...string) ([]byte, int) {
	command := exec.Command("bv", arguments...)
	command.Dir = a.dir
	command.Stdin = a.stdin
	command.Stderr = a.stderr
	command.Env = isolatedEnvironment(a.paths.Store, true)
	output, err := command.Output()
	if err == nil {
		return output, 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		if status, ok := exitError.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return output, 128 + int(status.Signal())
		}
		return output, exitError.ExitCode()
	}
	fmt.Fprintf(a.stderr, "wbd: executing bv: %v\n", err)
	return output, 1
}

func (a *app) runChild(directory, name string, arguments []string, viewer bool) int {
	command := exec.Command(name, arguments...)
	command.Dir = directory
	command.Stdin = a.stdin
	command.Stdout = a.stdout
	command.Stderr = a.stderr
	command.Env = isolatedEnvironment(a.paths.Store, viewer)
	err := command.Run()
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		if status, ok := exitError.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
		return exitError.ExitCode()
	}
	return a.fail(fmt.Errorf("running %s: %w", name, err))
}

func (a *app) fail(err error) int {
	if a.jsonFailure {
		code := "invalid_request"
		var policyErr *hub.PolicyError
		if errors.As(err, &policyErr) {
			code = string(policyErr.Code)
		}
		_ = json.NewEncoder(a.stderr).Encode(map[string]any{
			"error": map[string]string{"code": code, "message": err.Error()},
		})
		return 1
	}
	fmt.Fprintf(a.stderr, "wbd: %v\n", err)
	return 1
}

func isolatedEnvironment(store string, viewer bool) []string {
	environment := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !isolatedVariables[name] && !(viewer && name == "BV_NO_GITIGNORE") {
			environment = append(environment, entry)
		}
	}
	environment = append(environment, "BEADS_DIR="+store)
	if viewer {
		environment = append(environment, "BV_NO_GITIGNORE=1")
	}
	return environment
}

func need(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("required command not found: %s", name)
	}
	return nil
}

func commandSucceeded(directory, name string, arguments ...string) bool {
	command := exec.Command(name, arguments...)
	command.Dir = directory
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run() == nil
}

func appendJSON(arguments []string, json bool) []string {
	if json {
		return append(arguments, "--json")
	}
	return arguments
}
