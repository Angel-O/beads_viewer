package hub

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestListIssuesKeysetPagesSurviveInsertionAndTieBreakByID(t *testing.T) {
	dbPath := createListTestDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO issues (id, title, status, priority, issue_type, created_at, updated_at, closed_at) VALUES
		('a', 'A', 'closed', 2, 'task', '2026-08-01T00:00:00Z', '2026-08-03T00:00:00Z', '2026-08-03T00:00:00Z'),
		('b', 'B', 'closed', 2, 'task', '2026-08-01T00:00:00Z', '2026-08-02T00:00:00Z', '2026-08-03T00:00:00Z'),
		('c', 'C', 'closed', 2, 'task', '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z'),
		('open', 'Open', 'open', 2, 'task', '2026-08-01T00:00:00Z', '2026-08-04T00:00:00Z', NULL),
		('archived', 'Archived', 'open', 2, 'task', '2026-08-01T00:00:00Z', '2026-08-05T00:00:00Z', NULL)`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	first, err := ListIssues(filepath.Dir(dbPath), ListOptions{Statuses: []string{"closed"}, Sort: "closed_at:desc", Limit: 2, Paginate: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := issueIDs(first.Issues); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("first page IDs = %#v", got)
	}
	if first.Pagination == nil || !first.Pagination.HasMore || first.Pagination.NextCursor == "" {
		t.Fatalf("first pagination = %#v", first.Pagination)
	}

	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO issues (id, title, status, priority, issue_type, created_at, updated_at, closed_at) VALUES ('new', 'New', 'closed', 2, 'task', '2026-08-05T00:00:00Z', '2026-08-05T00:00:00Z', '2026-08-05T00:00:00Z')`)
	if closeErr := db.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}

	second, err := ListIssues(filepath.Dir(dbPath), ListOptions{Statuses: []string{"closed"}, Sort: "closed_at:desc", Limit: 2, Paginate: true, Cursor: first.Pagination.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if got := issueIDs(second.Issues); !reflect.DeepEqual(got, []string{"c"}) {
		t.Fatalf("second page IDs = %#v, want [c] after insertion: %#v", got, second)
	}
}

func TestListIssuesFiltersStrictRFC3339BoundaryAndBriefProjection(t *testing.T) {
	dbPath := createListTestDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO issues (id, title, status, priority, issue_type, assignee, created_at, updated_at, closed_at) VALUES
		('equal', 'Equal', 'open', 1, 'bug', 'agent', '2026-08-01T00:00:00Z', '2026-08-02T00:00:00Z', NULL),
		('after', 'After', 'open', 2, 'task', '', '2026-08-01T00:00:01Z', '2026-08-02T00:00:01Z', NULL),
		('tomb', 'Tombstone', 'tombstone', 0, 'task', '', '2026-09-01T00:00:00Z', '2026-09-01T00:00:00Z', NULL),
		('archived', 'Archived', 'open', 0, 'task', '', '2026-09-02T00:00:00Z', '2026-09-02T00:00:00Z', NULL);
		UPDATE issues SET tombstone = 1 WHERE id = 'archived'`)
	if closeErr := db.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	cutoff := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	page, err := ListIssues(filepath.Dir(dbPath), ListOptions{AfterCreatedAt: &cutoff, Limit: 10, Sort: "updated_at:desc", Brief: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := issueIDs(page.Issues); !reflect.DeepEqual(got, []string{"after"}) {
		t.Fatalf("strict after IDs = %#v", got)
	}
	data, err := json.Marshal(page.Issues[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"id":"after","title":"After","status":"open","priority":2,"issue_type":"task","updated_at":"2026-08-02T00:00:01Z"}` {
		t.Fatalf("brief projection = %s", data)
	}
}

func TestListIssuesReadyIgnoresArchivedBlockers(t *testing.T) {
	dbPath := createListTestDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO issues (id, title, status, priority, issue_type, created_at, updated_at) VALUES
		('dependent', 'Dependent', 'open', 2, 'task', '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z'),
		('archived-blocker', 'Archived blocker', 'open', 2, 'task', '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z');
		UPDATE issues SET tombstone = 1 WHERE id = 'archived-blocker';
		INSERT INTO dependencies (issue_id, depends_on_id, type) VALUES ('dependent', 'archived-blocker', 'blocks')`)
	if closeErr := db.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}

	page, err := ListIssues(filepath.Dir(dbPath), ListOptions{Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := issueIDs(page.Issues); !reflect.DeepEqual(got, []string{"dependent"}) {
		t.Fatalf("ready issue IDs = %#v", got)
	}
}

func TestListIssuesRejectsMalformedAndIncompatibleCursors(t *testing.T) {
	dbPath := createListTestDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO issues (id, title, status, priority, issue_type, created_at, updated_at) VALUES
		('one', 'One', 'open', 2, 'task', '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z'),
		('two', 'Two', 'open', 2, 'task', '2026-08-02T00:00:00Z', '2026-08-02T00:00:00Z')`)
	if closeErr := db.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	page, err := ListIssues(filepath.Dir(dbPath), ListOptions{Sort: "updated_at:desc", Limit: 1, Paginate: true})
	if err != nil {
		t.Fatal(err)
	}
	if page.Pagination == nil || !page.Pagination.HasMore || page.Pagination.NextCursor == "" {
		t.Fatalf("first pagination = %#v", page.Pagination)
	}
	_, err = ListIssues(filepath.Dir(dbPath), ListOptions{Sort: "closed_at:desc", Limit: 1, Paginate: true, Cursor: page.Pagination.NextCursor})
	if err == nil || !strings.Contains(err.Error(), "cursor was created for") {
		t.Fatalf("incompatible cursor error = %v", err)
	}
	_, err = ListIssues(filepath.Dir(dbPath), ListOptions{Limit: 1, Paginate: true, Cursor: "not-a-cursor"})
	if err == nil || !strings.Contains(err.Error(), "invalid cursor") {
		t.Fatalf("malformed cursor error = %v", err)
	}
}

func TestListIssuesExplicitPaginationReportsTerminalAndEmptyPages(t *testing.T) {
	dbPath := createListTestDB(t)
	empty, err := ListIssues(filepath.Dir(dbPath), ListOptions{Limit: 2, Paginate: true})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Pagination == nil || empty.Pagination.HasMore || empty.Pagination.NextCursor != "" {
		t.Fatalf("empty pagination = %#v", empty.Pagination)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO issues (id, title, status, priority, issue_type, created_at, updated_at) VALUES ('only', 'Only', 'open', 2, 'task', '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z')`)
	if closeErr := db.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := ListIssues(filepath.Dir(dbPath), ListOptions{Limit: 2, Paginate: true})
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Pagination == nil || terminal.Pagination.HasMore || terminal.Pagination.NextCursor != "" {
		t.Fatalf("terminal pagination = %#v", terminal.Pagination)
	}
}

func TestListIssuesPreservesFractionalPrecisionAndNormalizesCutoffs(t *testing.T) {
	dbPath := createListTestDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO issues (id, title, status, priority, issue_type, created_at, updated_at) VALUES
		('fraction-low', 'Fraction low', 'open', 2, 'task', '2026-08-01T00:00:00.123456789Z', '2026-08-01T00:00:00.123456789Z'),
		('fraction-high', 'Fraction high', 'open', 2, 'task', '2026-08-01T00:00:00.123456790Z', '2026-08-01T00:00:00.123456790Z')`)
	if closeErr := db.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}

	first, err := ListIssues(filepath.Dir(dbPath), ListOptions{Limit: 1, Paginate: true, Sort: "updated_at:desc"})
	if err != nil {
		t.Fatal(err)
	}
	if got := issueIDs(first.Issues); !reflect.DeepEqual(got, []string{"fraction-high"}) {
		t.Fatalf("fractional first page IDs = %#v", got)
	}
	second, err := ListIssues(filepath.Dir(dbPath), ListOptions{Limit: 1, Paginate: true, Sort: "updated_at:desc", Cursor: first.Pagination.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if got := issueIDs(second.Issues); !reflect.DeepEqual(got, []string{"fraction-low"}) {
		t.Fatalf("fractional second page IDs = %#v", got)
	}

	cutoff := time.Date(2026, time.August, 1, 2, 0, 0, 123456789, time.FixedZone("cutoff", 2*60*60))
	filtered, err := ListIssues(filepath.Dir(dbPath), ListOptions{AfterUpdatedAt: &cutoff, Sort: "updated_at:desc"})
	if err != nil {
		t.Fatal(err)
	}
	if got := issueIDs(filtered.Issues); !reflect.DeepEqual(got, []string{"fraction-high"}) {
		t.Fatalf("timezone-equivalent cutoff IDs = %#v", got)
	}
}

func createListTestDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "beads.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			description TEXT,
			status TEXT NOT NULL,
			priority INTEGER,
			issue_type TEXT,
			assignee TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			closed_at DATETIME,
			tombstone INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE labels (issue_id TEXT NOT NULL, label TEXT NOT NULL);
		CREATE TABLE dependencies (issue_id TEXT NOT NULL, depends_on_id TEXT NOT NULL, type TEXT);
	`)
	if closeErr := db.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func issueIDs(issues []ListIssue) []string {
	ids := make([]string, len(issues))
	for index, issue := range issues {
		ids[index] = issue.ID
	}
	return ids
}
