package correlation

import (
	"strings"
	"testing"
)

func TestExternalHistorySnapshotValidatesNeutralSourceShape(t *testing.T) {
	valid := ExternalHistorySnapshot{
		Store:        "/hub/.beads",
		Ledger:       "/hub/correlations.jsonl",
		Repositories: map[string]string{"ctx:source": "/source"},
		Correlations: []ExternalHistoryCorrelation{{BeadID: "item-1", Context: "ctx:source", Commit: strings.Repeat("a", 40)}},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid snapshot rejected: %v", err)
	}
	invalid := valid
	invalid.Correlations = []ExternalHistoryCorrelation{{BeadID: "item-1", Context: "ctx:missing", Commit: "short"}}
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid snapshot accepted")
	}
}
