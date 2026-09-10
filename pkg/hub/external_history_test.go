package hub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
)

func TestExternalHistorySourceLoadsValidatedSnapshot(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, "private", "correlations.jsonl")
	config := filepath.Join(root, "hub.yaml")
	data := "version: 1\nstore: store\nledger: private/correlations.jsonl\nrepositories:\n  ctx:source:\n    path: source\n"
	if err := os.MkdirAll(filepath.Join(root, "source"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(ledger), 0o700); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	if err := os.WriteFile(ledger, []byte(`{"bead_id":"item-1","context":"ctx:source","commit":"`+sha+`"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewExternalHistorySource(config)([]correlation.BeadInfo{{ID: "item-1", Labels: []string{"ctx:source"}}})
	if err != nil {
		t.Fatalf("load external snapshot: %v", err)
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("snapshot validation: %v", err)
	}
	if snapshot.Store != filepath.Join(root, "store") || snapshot.Ledger != ledger || len(snapshot.Correlations) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}
