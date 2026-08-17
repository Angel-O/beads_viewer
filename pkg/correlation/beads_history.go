package correlation

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

type beadsHistorySnapshot struct {
	CommitHash string `json:"CommitHash"`
	Committer  string `json:"Committer"`
	CommitDate string `json:"CommitDate"`
	Issue      struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Status    string `json:"status"`
		CreatedAt string `json:"created_at"`
		CreatedBy string `json:"created_by"`
	} `json:"Issue"`
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
		sort.SliceStable(snapshots, func(i, j int) bool {
			return snapshots[i].CommitDate < snapshots[j].CommitDate
		})
		previousStatus := ""
		for i, snapshot := range snapshots {
			timestamp, err := time.Parse(time.RFC3339, snapshot.CommitDate)
			if err != nil {
				return nil, fmt.Errorf("parsing Beads lifecycle timestamp %q for %q: %w", snapshot.CommitDate, bead.ID, err)
			}
			eventType := lifecycleEventType(previousStatus, snapshot.Issue.Status, i == 0)
			previousStatus = snapshot.Issue.Status
			if eventType == "" || !withinHistoryRange(timestamp, opts) {
				continue
			}
			events = append(events, BeadEvent{
				BeadID:    bead.ID,
				EventType: eventType,
				Timestamp: timestamp,
				CommitSHA: snapshot.CommitHash,
				CommitMsg: fmt.Sprintf("Beads status: %s", snapshot.Issue.Status),
				Author:    snapshot.Committer,
			})
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Timestamp.Before(events[j].Timestamp) })
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
