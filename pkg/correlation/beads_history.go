package correlation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	json "github.com/goccy/go-json"
)

type beadsHistorySnapshot struct {
	CommitHash string          `json:"CommitHash"`
	Committer  string          `json:"Committer"`
	CommitDate string          `json:"CommitDate"`
	Issue      json.RawMessage `json:"Issue"`
}

type beadsBulkHistoryEnvelope struct {
	SchemaVersion int                     `json:"schema_version"`
	Issues        []beadsBulkHistoryIssue `json:"issues"`
}

type beadsBulkHistoryIssue struct {
	IssueID   string                 `json:"issue_id"`
	Snapshots []beadsHistorySnapshot `json:"snapshots"`
}

const (
	beadsBulkHistorySchemaVersion = 1
	beadsBulkHistoryMaxIDs        = 1000
	beadsBulkHistoryMaxIDLength   = 255
	// Bulk history can contain many full issue snapshots, but provider output
	// must remain bounded so one malformed response cannot exhaust Viewer memory.
	beadsBulkHistoryMaxResponseBytes = 64 << 20
	beadsBulkHistoryMaxStderrBytes   = 64 << 10
)

type boundedDiagnostic struct {
	data      []byte
	limit     int
	truncated bool
}

func (b *boundedDiagnostic) Write(p []byte) (int, error) {
	remaining := b.limit - len(b.data)
	if remaining > 0 {
		b.data = append(b.data, p[:min(remaining, len(p))]...)
	}
	if len(p) > remaining {
		b.truncated = true
	}
	return len(p), nil
}

func (b *boundedDiagnostic) String() string {
	diagnostic := strings.TrimSpace(string(b.data))
	if b.truncated {
		return diagnostic + " [stderr truncated]"
	}
	return diagnostic
}

type beadsIssue struct {
	ID        string   `json:"id"`
	IssueType string   `json:"issue_type"`
	Labels    []string `json:"labels"`
}

func validateBeadContext(store, beadID, contextKey string) error {
	cmd := exec.Command("bd", "--db", store, "--readonly", "show", beadID, "--json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("validating bead %q in configured store %q: %w: %s", beadID, store, err, strings.TrimSpace(string(out)))
	}
	var issues []beadsIssue
	if err := json.Unmarshal(out, &issues); err != nil {
		return fmt.Errorf("parsing bead %q from configured store %q: %w", beadID, store, err)
	}
	if len(issues) == 0 || issues[0].ID != beadID {
		return fmt.Errorf("bead %q was not found in configured store %q", beadID, store)
	}
	issueType := strings.TrimSpace(issues[0].IssueType)
	if issueType == "" {
		return fmt.Errorf("bead %q does not provide a non-empty issue_type", beadID)
	}
	if strings.EqualFold(issueType, "todo") {
		return fmt.Errorf("bead %q is a todo and cannot be correlated with a Git commit", beadID)
	}
	for _, label := range issues[0].Labels {
		if label == contextKey {
			return nil
		}
	}
	return fmt.Errorf("bead %q in configured store %q does not carry context label %q", beadID, store, contextKey)
}

func loadBeadsLifecycle(ctx context.Context, store string, beads []BeadInfo, opts CorrelatorOptions) ([]BeadEvent, error) {
	selected := make([]BeadInfo, 0, len(beads))
	for _, bead := range beads {
		if opts.BeadID == "" || opts.BeadID == bead.ID {
			selected = append(selected, bead)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].ID < selected[j].ID })
	if len(selected) == 0 {
		return nil, nil
	}

	ids := make([]string, len(selected))
	for i, bead := range selected {
		ids[i] = bead.ID
	}
	if err := validateBulkHistoryRequestIDs(ids); err != nil {
		return nil, fmt.Errorf("loading bulk Beads lifecycle from %q: %w", store, err)
	}

	commandContext := ctx
	if commandContext == nil {
		commandContext = context.Background()
	}
	cmd := exec.CommandContext(commandContext, "bd", "--db", store, "--readonly", "history", "--ids-file", "-", "--json")
	cmd.Stdin = strings.NewReader(strings.Join(ids, "\n") + "\n")
	out, diagnostic, err := runBulkHistoryCommand(cmd)
	if err != nil {
		if commandContext.Err() != nil {
			return nil, fmt.Errorf("loading bulk Beads lifecycle from %q: %w", store, commandContext.Err())
		}
		var executableErr *exec.Error
		if errors.As(err, &executableErr) {
			return nil, fmt.Errorf("loading bulk Beads lifecycle from %q: Beads executable unavailable: %w", store, err)
		}
		if isBulkHistoryCapabilityDiagnostic(diagnostic) {
			return nil, fmt.Errorf("loading bulk Beads lifecycle from %q: bulk History support is required; install a Beads CLI that supports 'history --ids-file - --json': %w: %s", store, err, diagnostic)
		}
		if diagnostic != "" {
			return nil, fmt.Errorf("loading bulk Beads lifecycle from %q: %w: %s", store, err, diagnostic)
		}
		return nil, fmt.Errorf("loading bulk Beads lifecycle from %q: %w", store, err)
	}

	groups, err := parseBulkBeadsHistory(out, ids, store)
	if err != nil {
		return nil, err
	}

	var events []BeadEvent
	for i, bead := range selected {
		beadEvents, err := lifecycleEventsFromSnapshots(bead.ID, groups[i].Snapshots, opts)
		if err != nil {
			return nil, err
		}
		events = append(events, beadEvents...)
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Timestamp.Before(events[j].Timestamp) })
	return events, nil
}

func runBulkHistoryCommand(cmd *exec.Cmd) ([]byte, string, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", fmt.Errorf("opening bulk History stdout: %w", err)
	}
	stderr := boundedDiagnostic{limit: beadsBulkHistoryMaxStderrBytes}
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, stderr.String(), err
	}

	output, readErr := io.ReadAll(io.LimitReader(stdout, beadsBulkHistoryMaxResponseBytes+1))
	if len(output) > beadsBulkHistoryMaxResponseBytes {
		_ = cmd.Process.Kill()
		_ = stdout.Close()
		_ = cmd.Wait()
		return nil, stderr.String(), fmt.Errorf("bulk History response too large: exceeds %d bytes", beadsBulkHistoryMaxResponseBytes)
	}
	if readErr != nil {
		_ = cmd.Process.Kill()
		_ = stdout.Close()
		waitErr := cmd.Wait()
		if waitErr != nil {
			return nil, stderr.String(), fmt.Errorf("reading bulk History response: %w (process: %v)", readErr, waitErr)
		}
		return nil, stderr.String(), fmt.Errorf("reading bulk History response: %w", readErr)
	}
	if err := cmd.Wait(); err != nil {
		return nil, stderr.String(), err
	}
	// Decode only after Wait so malformed JSON cannot leave a provider process
	// running or holding a pipe while validation returns an error.
	return output, stderr.String(), nil
}

func isBulkHistoryCapabilityDiagnostic(diagnostic string) bool {
	diagnostic = strings.ToLower(diagnostic)
	return strings.Contains(diagnostic, "unknown flag: --ids-file") ||
		strings.Contains(diagnostic, "unknown command \"history\"") ||
		strings.Contains(diagnostic, "does not support bulk history") ||
		strings.Contains(diagnostic, "bulk history is not supported")
}

func validateBulkHistoryRequestIDs(ids []string) error {
	if len(ids) > beadsBulkHistoryMaxIDs {
		return fmt.Errorf("bulk History accepts at most %d issue IDs, got %d", beadsBulkHistoryMaxIDs, len(ids))
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if err := validateBulkHistoryID(id); err != nil {
			return err
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("duplicate bulk History issue ID %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validateBulkHistoryID(id string) error {
	if id == "" || !utf8.ValidString(id) || strings.TrimSpace(id) != id || strings.ContainsAny(id, "\r\n") {
		return fmt.Errorf("malformed bulk History issue ID %q", id)
	}
	if length := utf8.RuneCountInString(id); length > beadsBulkHistoryMaxIDLength {
		return fmt.Errorf("bulk History issue ID %q is %d characters (max %d)", id, length, beadsBulkHistoryMaxIDLength)
	}
	return nil
}

func parseBulkBeadsHistory(data []byte, requestedIDs []string, store string) ([]beadsBulkHistoryIssue, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var envelope beadsBulkHistoryEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("parsing bulk Beads lifecycle from %q: %w", store, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("parsing bulk Beads lifecycle from %q: %w", store, err)
	}
	if envelope.SchemaVersion != beadsBulkHistorySchemaVersion {
		return nil, fmt.Errorf("parsing bulk Beads lifecycle from %q: unsupported schema_version %d (expected %d)", store, envelope.SchemaVersion, beadsBulkHistorySchemaVersion)
	}
	if envelope.Issues == nil {
		return nil, fmt.Errorf("parsing bulk Beads lifecycle from %q: issues must be a JSON array", store)
	}

	requested := make(map[string]struct{}, len(requestedIDs))
	for _, id := range requestedIDs {
		requested[id] = struct{}{}
	}
	if len(envelope.Issues) > len(requestedIDs) {
		return nil, fmt.Errorf("parsing bulk Beads lifecycle from %q: response has %d issue groups for %d requested IDs", store, len(envelope.Issues), len(requestedIDs))
	}
	seen := make(map[string]struct{}, len(envelope.Issues))
	for i := range envelope.Issues {
		group := &envelope.Issues[i]
		if err := validateBulkHistoryID(group.IssueID); err != nil {
			return nil, fmt.Errorf("parsing bulk Beads lifecycle from %q: response group %d: %w", store, i, err)
		}
		if _, exists := seen[group.IssueID]; exists {
			return nil, fmt.Errorf("parsing bulk Beads lifecycle from %q: duplicate issue group %q", store, group.IssueID)
		}
		seen[group.IssueID] = struct{}{}
		if _, expected := requested[group.IssueID]; !expected {
			return nil, fmt.Errorf("parsing bulk Beads lifecycle from %q: unexpected issue group %q", store, group.IssueID)
		}
		if group.Snapshots == nil {
			return nil, fmt.Errorf("parsing bulk Beads lifecycle for %q from %q: snapshots must be a JSON array", group.IssueID, store)
		}
		for snapshotIndex := range group.Snapshots {
			if err := validateBulkHistorySnapshot(group.IssueID, group.Snapshots[snapshotIndex]); err != nil {
				return nil, fmt.Errorf("parsing bulk Beads lifecycle for %q from %q snapshot %d: %w", group.IssueID, store, snapshotIndex, err)
			}
		}
	}
	for _, id := range requestedIDs {
		if _, exists := seen[id]; !exists {
			return nil, fmt.Errorf("parsing bulk Beads lifecycle from %q: missing requested issue group %q", store, id)
		}
	}
	for i, id := range requestedIDs {
		if envelope.Issues[i].IssueID != id {
			return nil, fmt.Errorf("parsing bulk Beads lifecycle from %q: issue group %d is %q, expected %q", store, i, envelope.Issues[i].IssueID, id)
		}
	}
	return envelope.Issues, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func validateBulkHistorySnapshot(groupID string, snapshot beadsHistorySnapshot) error {
	if snapshot.CommitHash == "" {
		return fmt.Errorf("missing CommitHash")
	}
	if _, err := time.Parse(time.RFC3339, snapshot.CommitDate); err != nil {
		return fmt.Errorf("parsing Beads lifecycle timestamp %q: %w", snapshot.CommitDate, err)
	}
	if len(snapshot.Issue) == 0 || bytes.Equal(bytes.TrimSpace(snapshot.Issue), []byte("null")) {
		return fmt.Errorf("Issue must be a JSON object")
	}
	var issue map[string]any
	if err := json.Unmarshal(snapshot.Issue, &issue); err != nil {
		return fmt.Errorf("parsing Beads lifecycle issue snapshot: %w", err)
	}
	if issue == nil {
		return fmt.Errorf("Issue must be a JSON object")
	}
	issueID, ok := issue["id"].(string)
	if !ok || issueID != groupID {
		return fmt.Errorf("issue snapshot ID %q does not match group ID %q", issueID, groupID)
	}
	if _, ok := issue["status"].(string); !ok {
		return fmt.Errorf("issue snapshot for %q has invalid status", groupID)
	}
	return nil
}

func lifecycleEventsFromSnapshots(beadID string, snapshots []beadsHistorySnapshot, opts CorrelatorOptions) ([]BeadEvent, error) {
	sort.SliceStable(snapshots, func(i, j int) bool {
		return snapshots[i].CommitDate < snapshots[j].CommitDate
	})

	var events []BeadEvent
	var previousIssue any
	previousStatus := ""
	for _, snapshot := range snapshots {
		timestamp, err := time.Parse(time.RFC3339, snapshot.CommitDate)
		if err != nil {
			return nil, fmt.Errorf("parsing Beads lifecycle timestamp %q for %q: %w", snapshot.CommitDate, beadID, err)
		}

		var issue map[string]any
		if err := json.Unmarshal(snapshot.Issue, &issue); err != nil {
			return nil, fmt.Errorf("parsing Beads lifecycle issue snapshot for %q: %w", beadID, err)
		}
		status, _ := issue["status"].(string)
		first := previousIssue == nil
		if !first && reflect.DeepEqual(previousIssue, issue) {
			continue
		}

		eventType := lifecycleEventType(previousStatus, status, first)
		previousIssue = issue
		previousStatus = status
		if eventType == "" || !withinHistoryRange(timestamp, opts) {
			continue
		}
		events = append(events, BeadEvent{
			BeadID:    beadID,
			EventType: eventType,
			Timestamp: timestamp,
			CommitSHA: snapshot.CommitHash,
			CommitMsg: fmt.Sprintf("Beads status: %s", status),
			Author:    snapshot.Committer,
		})
	}
	return events, nil
}

func lifecycleEventType(previous, current string, first bool) EventType {
	if first {
		return EventCreated
	}
	if previous == current {
		return EventModified
	}
	if current == "in_progress" {
		return EventClaimed
	}
	if isClosedLifecycleStatus(current) {
		return EventClosed
	}
	if isClosedLifecycleStatus(previous) {
		return EventReopened
	}
	return EventModified
}

func withinHistoryRange(timestamp time.Time, opts CorrelatorOptions) bool {
	if opts.Since != nil && timestamp.Before(*opts.Since) {
		return false
	}
	if opts.Until != nil && timestamp.After(*opts.Until) {
		return false
	}
	return true
}
