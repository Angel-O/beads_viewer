package correlation

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestSnapshotMatchesLegacyPatch is a differential test: extractViaSnapshots
// must produce the same BeadEvents as the legacy extractViaGitLogPatch on a real
// repo. Point it at a beads repo via BV_DIFFCHECK_REPO; skipped otherwise.
func TestSnapshotMatchesLegacyPatch(t *testing.T) {
	repo := os.Getenv("BV_DIFFCHECK_REPO")
	if repo == "" {
		t.Skip("set BV_DIFFCHECK_REPO to a beads git repo to run the differential check")
	}
	e := NewExtractor(repo)
	opts := ExtractOptions{Limit: 200}

	legacy, err := e.extractViaGitLogPatch(opts)
	if err != nil {
		t.Fatalf("legacy: %v", err)
	}
	snap, err := e.extractViaSnapshots(opts)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	key := func(ev BeadEvent) string {
		return fmt.Sprintf("%s|%s|%s|%s", ev.CommitSHA, ev.BeadID, ev.EventType, ev.Timestamp.Format("2006-01-02T15:04:05Z07:00"))
	}
	ls := make([]string, 0, len(legacy))
	for _, ev := range legacy {
		ls = append(ls, key(ev))
	}
	ss := make([]string, 0, len(snap))
	for _, ev := range snap {
		ss = append(ss, key(ev))
	}
	sort.Strings(ls)
	sort.Strings(ss)

	t.Logf("legacy events=%d snapshot events=%d", len(ls), len(ss))
	if len(ls) != len(ss) {
		t.Fatalf("event count mismatch: legacy=%d snapshot=%d", len(ls), len(ss))
	}
	for i := range ls {
		if ls[i] != ss[i] {
			t.Fatalf("event mismatch at %d:\n legacy=%s\n snap  =%s", i, ls[i], ss[i])
		}
	}
}

func TestBlobReaderReusesOnlyRecycledBuffers(t *testing.T) {
	contentForStatus := func(status string) []byte {
		padding := strings.Repeat("x", 64-len(status))
		return []byte(fmt.Sprintf("{\"id\":\"bv-a\",\"status\":%q,\"title\":%q}\n", status, padding))
	}
	oldContent := contentForStatus("open")
	currentContent := contentForStatus("in_progress")
	nextContent := contentForStatus("closed")

	var protocol bytes.Buffer
	protocol.WriteString("missing-before-first-valid missing\n")
	for i, content := range [][]byte{oldContent, currentContent, nextContent} {
		fmt.Fprintf(&protocol, "oid-%d blob %d\n", i, len(content))
		protocol.Write(content)
		protocol.WriteByte('\n')
	}
	protocol.WriteString("missing-after-recycle missing\n")

	var requests bytes.Buffer
	reader := &blobReader{
		w:           bufio.NewWriter(&requests),
		out:         bufio.NewReaderSize(&protocol, blobReaderBufferSize),
		arenaRunway: blobReaderArenaRunway,
	}

	missing, err := reader.read("missing-before-first-valid")
	if err != nil {
		t.Fatalf("read missing blob before first valid blob: %v", err)
	}
	if missing != nil {
		t.Fatalf("missing blob before first valid blob = %q, want nil", missing)
	}
	if reader.arenaRunway != blobReaderArenaRunway {
		t.Fatalf("missing blob consumed arena runway: got %d, want %d", reader.arenaRunway, blobReaderArenaRunway)
	}

	oldBlob, err := reader.read("oid-0")
	if err != nil {
		t.Fatalf("read old blob: %v", err)
	}
	if got, want := cap(oldBlob), len(oldBlob)+blobReaderArenaRunway; got != want {
		t.Fatalf("first valid blob capacity = %d, want payload %d + runway %d = %d", got, len(oldBlob), blobReaderArenaRunway, want)
	}
	if got, want := reader.out.Size()+cap(oldBlob), gitLogMaxScanTokenSize+len(oldBlob); got != want {
		t.Fatalf("transport + first arena capacity = %d, want prior transport + payload = %d", got, want)
	}
	if reader.arenaRunway != 0 {
		t.Fatalf("first valid blob left arena runway = %d, want 0", reader.arenaRunway)
	}
	oldSet := newRecordLineSet(oldBlob)
	currentBlob, err := reader.read("oid-1")
	if err != nil {
		t.Fatalf("read current blob: %v", err)
	}
	if &oldBlob[0] == &currentBlob[0] {
		t.Fatal("reader reused a buffer whose record-line set was still live")
	}
	currentSet := newRecordLineSet(currentBlob)
	claimed := NewExtractor("/tmp/test").parseDiff(
		synthesizeRecordDiff(oldSet, currentSet), commitInfo{}, "",
	)
	if len(claimed) != 1 || claimed[0].BeadID != "bv-a" || claimed[0].EventType != EventClaimed {
		t.Fatalf("old/current diff changed before recycle: %#v", claimed)
	}

	currentBefore := append([]byte(nil), currentBlob...)
	reader.recycle(oldBlob)
	nextBlob, err := reader.read("oid-2")
	if err != nil {
		t.Fatalf("read next blob: %v", err)
	}
	if &nextBlob[0] != &oldBlob[0] {
		t.Fatal("reader did not reuse the explicitly recycled buffer")
	}
	if cap(nextBlob) != cap(oldBlob) {
		t.Fatalf("reused buffer capacity = %d, want preserved arena capacity %d", cap(nextBlob), cap(oldBlob))
	}
	if !bytes.Equal(currentBlob, currentBefore) {
		t.Fatal("reusing the old buffer overwrote the still-live current blob")
	}
	closed := NewExtractor("/tmp/test").parseDiff(
		synthesizeRecordDiff(currentSet, newRecordLineSet(nextBlob)), commitInfo{}, "",
	)
	if len(closed) != 1 || closed[0].BeadID != "bv-a" || closed[0].EventType != EventClosed {
		t.Fatalf("current/next diff changed after reuse: %#v", closed)
	}

	reader.recycle(nextBlob)
	missing, err = reader.read("missing-after-recycle")
	if err != nil {
		t.Fatalf("read missing blob: %v", err)
	}
	if missing != nil {
		t.Fatalf("missing blob = %q, want nil", missing)
	}
	if cap(reader.spare) == 0 {
		t.Fatal("missing blob consumed the reusable buffer")
	}
}

func TestSnapshotBufferReusePreservesOverlappingHistory(t *testing.T) {
	repo := initTempGitRepo(t)
	t.Setenv("BV_ROBOT", "1")
	t.Setenv("BV_NO_CACHE", "1")
	t.Setenv("BV_CACHE_DIR", t.TempDir())

	beadsDir := filepath.Join(repo, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("create beads dir: %v", err)
	}
	beadsPath := filepath.Join(beadsDir, "issues.jsonl")
	writeState := func(status, message string) {
		t.Helper()
		// Equal-size snapshots force each evicted buffer to be eligible for the
		// next read while the shared boundary blob remains live.
		padding := strings.Repeat("x", 128*1024-len(status))
		content := fmt.Sprintf("{\"id\":\"bv-a\",\"status\":%q,\"title\":%q}\n", status, padding)
		if err := os.WriteFile(beadsPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", status, err)
		}
		runGit(t, repo, "add", ".beads/issues.jsonl")
		runGit(t, repo, "commit", "-m", message)
	}

	writeState("open", "create bead")
	writeState("in_progress", "claim bead")
	writeState("closed", "close bead")
	writeState("open", "reopen bead")

	e := NewExtractor(repo)
	configuredReader, err := e.newBlobReader()
	if err != nil {
		t.Fatalf("create configured blob reader: %v", err)
	}
	if got := configuredReader.out.Size(); got != blobReaderBufferSize {
		t.Errorf("configured blob reader transport capacity = %d, want %d", got, blobReaderBufferSize)
	}
	if got := configuredReader.arenaRunway; got != blobReaderArenaRunway {
		t.Errorf("configured blob reader arena runway = %d, want %d", got, blobReaderArenaRunway)
	}
	if err := configuredReader.Close(); err != nil {
		t.Fatalf("close configured blob reader: %v", err)
	}

	opts := ExtractOptions{Limit: 10}
	legacy, err := e.extractViaGitLogPatch(opts)
	if err != nil {
		t.Fatalf("legacy extraction: %v", err)
	}
	snapshot, err := e.extractViaSnapshots(opts)
	if err != nil {
		t.Fatalf("snapshot extraction: %v", err)
	}
	assertEventsByteIdentical(t, legacy, snapshot, "recycled overlapping window")

	wantTypes := []EventType{EventCreated, EventClaimed, EventClosed, EventReopened}
	if len(snapshot) != len(wantTypes) {
		t.Fatalf("snapshot event count = %d, want %d", len(snapshot), len(wantTypes))
	}
	for i, want := range wantTypes {
		if snapshot[i].BeadID != "bv-a" || snapshot[i].EventType != want {
			t.Fatalf("snapshot event %d = (%q, %q), want (bv-a, %q)", i, snapshot[i].BeadID, snapshot[i].EventType, want)
		}
	}
}
