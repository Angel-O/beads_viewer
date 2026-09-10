package correlation

import (
	"fmt"
	"regexp"
	"strings"
)

var fullCommitSHARegex = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

// ExternalHistoryCorrelation identifies one bead and one immutable source
// commit. It is neutral so any validated source can provide it.
type ExternalHistoryCorrelation struct {
	BeadID  string `json:"bead_id"`
	Context string `json:"context"`
	Commit  string `json:"commit"`
}

// ExternalHistorySnapshot is the validated, read-only input used by external
// history correlation. Its source owns configuration and ledger persistence.
type ExternalHistorySnapshot struct {
	Store        string
	Ledger       string
	Repositories map[string]string
	Correlations []ExternalHistoryCorrelation
}

// ExternalHistorySource supplies one validated snapshot for the current bead
// set, keeping source-specific policy out of correlation algorithms.
type ExternalHistorySource func([]BeadInfo) (ExternalHistorySnapshot, error)

// Validate checks the source shape at the algorithm boundary.
func (s ExternalHistorySnapshot) Validate() error {
	if strings.TrimSpace(s.Store) == "" || strings.TrimSpace(s.Ledger) == "" {
		return fmt.Errorf("external history snapshot requires store and ledger paths")
	}
	for i, record := range s.Correlations {
		if strings.TrimSpace(record.BeadID) == "" || strings.TrimSpace(record.Context) == "" || strings.TrimSpace(record.Commit) == "" {
			return fmt.Errorf("external history snapshot correlation %d requires non-empty bead_id, context, and commit", i+1)
		}
		if _, ok := s.Repositories[record.Context]; !ok {
			return fmt.Errorf("external history snapshot correlation %d references undefined context %q", i+1, record.Context)
		}
		if !fullCommitSHARegex.MatchString(record.Commit) {
			return fmt.Errorf("external history snapshot correlation %d commit %q must be a full 40- or 64-character Git object ID", i+1, record.Commit)
		}
	}
	return nil
}
