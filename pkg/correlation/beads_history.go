package correlation

import (
	"context"
	"fmt"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

type beadsHistorySnapshot struct {
	CommitHash string          `json:"CommitHash"`
	Committer  string          `json:"Committer"`
	CommitDate string          `json:"CommitDate"`
	Issue      json.RawMessage `json:"Issue"`
}

type beadsIssue struct {
	ID     string   `json:"id"`
	Labels []string `json:"labels"`
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

	var events []BeadEvent
	for _, bead := range selected {
		commandContext := ctx
		if commandContext == nil {
			commandContext = context.Background()
		}
		cmd := exec.CommandContext(commandContext, "bd", "--db", store, "--readonly", "history", bead.ID, "--json")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("loading Beads lifecycle for %q from %q: %w: %s", bead.ID, store, err, strings.TrimSpace(string(out)))
		}
		var snapshots []beadsHistorySnapshot
		if err := json.Unmarshal(out, &snapshots); err != nil {
			return nil, fmt.Errorf("parsing Beads lifecycle for %q from %q: %w", bead.ID, store, err)
		}
		beadEvents, err := lifecycleEventsFromSnapshots(bead.ID, snapshots, opts)
		if err != nil {
			return nil, err
		}
		events = append(events, beadEvents...)
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Timestamp.Before(events[j].Timestamp) })
	return events, nil
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
