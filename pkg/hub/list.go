package hub

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ListOptions describes the bounded, database-backed list API. A zero Limit
// means no limit unless Paginate is true.
type ListOptions struct {
	Context        string
	AllContexts    bool
	Statuses       []string
	IssueType      string
	Priority       *int
	Labels         []string
	Ready          bool
	Limit          int
	Paginate       bool
	Cursor         string
	Sort           string
	AfterCreatedAt *time.Time
	AfterUpdatedAt *time.Time
	AfterClosedAt  *time.Time
	Brief          bool
}

// ListIssue is the fixed output shape for database-backed list requests.
// Brief requests populate only the fields documented by the compact shape.
type ListIssue struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status"`
	Priority    int      `json:"priority"`
	IssueType   string   `json:"issue_type"`
	Assignee    string   `json:"assignee,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
	ClosedAt    *string  `json:"closed_at,omitempty"`
}

// ListPagination is present only for explicitly paginated requests.
type ListPagination struct {
	Limit      int    `json:"limit"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// ListPage is the result of a database-backed list query.
type ListPage struct {
	Issues     []ListIssue
	Pagination *ListPagination
}

type listRow struct {
	issue     ListIssue
	orderTime string
	orderNull bool
}

type listCursor struct {
	Version int    `json:"v"`
	Sort    string `json:"sort"`
	Value   string `json:"value,omitempty"`
	Null    bool   `json:"null,omitempty"`
	ID      string `json:"id"`
	Filter  string `json:"filter"`
}

// ListIssues executes a bounded keyset query against the Hub's SQLite store.
// It never loads rows that do not satisfy the SQL filters and fetches at most
// limit+1 rows when pagination is requested.
func ListIssues(store string, opts ListOptions) (ListPage, error) {
	if opts.Limit < 0 {
		return ListPage{}, errors.New("list limit cannot be negative")
	}
	paginated := opts.Paginate || opts.Cursor != ""
	if paginated && opts.Limit == 0 {
		return ListPage{}, errors.New("paginated list requires a positive limit")
	}
	sortKey, err := normalizeListSort(opts.Sort)
	if err != nil {
		return ListPage{}, err
	}
	if opts.Cursor != "" && opts.Limit == 0 {
		return ListPage{}, errors.New("a cursor requires a positive limit")
	}

	dbPath := filepath.Join(store, "beads.db")
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ListPage{}, fmt.Errorf("bounded list requires the SQLite Hub database at %s; this store has no beads.db", dbPath)
		}
		return ListPage{}, fmt.Errorf("checking Hub database: %w", err)
	}
	db, err := sql.Open("sqlite", sqliteReadOnlyDSN(dbPath))
	if err != nil {
		return ListPage{}, fmt.Errorf("opening Hub database: %w", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return ListPage{}, fmt.Errorf("connecting to Hub database: %w", err)
	}
	_, _ = db.Exec("PRAGMA busy_timeout = 5000")
	_, _ = db.Exec("PRAGMA query_only = ON")

	columns, err := tableColumns(db, "issues")
	if err != nil {
		return ListPage{}, err
	}
	for _, column := range []string{"id", "title", "status"} {
		if !columns[column] {
			return ListPage{}, fmt.Errorf("Hub database issues table is missing required column %q", column)
		}
	}
	if sortKey != "" && !columns[sortField(sortKey)] {
		return ListPage{}, fmt.Errorf("Hub database cannot sort by %s: issues table has no %q column", sortKey, sortField(sortKey))
	}
	for name, requested := range map[string]*time.Time{
		"created_at": opts.AfterCreatedAt,
		"updated_at": opts.AfterUpdatedAt,
		"closed_at":  opts.AfterClosedAt,
	} {
		if requested != nil && !columns[name] {
			return ListPage{}, fmt.Errorf("Hub database cannot filter by %s: issues table has no %q column", name, name)
		}
	}

	labelsColumn := columns["labels"]
	labelsTable, err := hasTable(db, "labels")
	if err != nil {
		return ListPage{}, err
	}
	if (!opts.AllContexts && opts.Context != "") || len(opts.Labels) > 0 {
		if !labelsColumn && !labelsTable {
			return ListPage{}, errors.New("Hub database has no labels column or labels table for the requested label filter")
		}
	}

	where := []string{"i.status <> 'tombstone'"}
	args := make([]any, 0)
	add := func(clause string, values ...any) {
		where = append(where, clause)
		args = append(args, values...)
	}
	if columns["tombstone"] {
		add("(i.tombstone IS NULL OR i.tombstone = 0)")
	}
	if !opts.AllContexts && opts.Context != "" {
		add(labelExistsClause(labelsColumn, labelsTable), opts.Context)
	}
	if len(opts.Statuses) > 0 {
		placeholders := make([]string, len(opts.Statuses))
		for index, status := range opts.Statuses {
			placeholders[index] = "?"
			args = append(args, status)
		}
		where = append(where, "i.status IN ("+strings.Join(placeholders, ",")+")")
	}
	if opts.IssueType != "" {
		if !columns["issue_type"] {
			return ListPage{}, errors.New("Hub database cannot filter by issue type: issues table has no \"issue_type\" column")
		}
		add("i.issue_type = ?", opts.IssueType)
	}
	if opts.Priority != nil {
		if !columns["priority"] {
			return ListPage{}, errors.New("Hub database cannot filter by priority: issues table has no \"priority\" column")
		}
		add("i.priority = ?", *opts.Priority)
	}
	for _, label := range opts.Labels {
		add(labelExistsClause(labelsColumn, labelsTable), label)
	}
	if opts.Ready {
		add("i.status = 'open'")
		dependencyColumns, dependencyErr := tableColumns(db, "dependencies")
		if dependencyErr != nil {
			return ListPage{}, dependencyErr
		}
		if !dependencyColumns["issue_id"] || !dependencyColumns["depends_on_id"] {
			return ListPage{}, errors.New("--ready requires dependencies.issue_id and dependencies.depends_on_id in the Hub database")
		}
		dependencyType := "''"
		if dependencyColumns["dependency_type"] {
			dependencyType = "d.dependency_type"
		} else if dependencyColumns["type"] {
			dependencyType = "d.type"
		}
		blockerActive := "blocker.status NOT IN ('closed', 'tombstone')"
		if columns["tombstone"] {
			blockerActive += " AND (blocker.tombstone IS NULL OR blocker.tombstone = 0)"
		}
		add("NOT EXISTS (SELECT 1 FROM dependencies d JOIN issues blocker ON blocker.id = d.depends_on_id WHERE d.issue_id = i.id AND (" + dependencyType + " = '' OR " + dependencyType + " = 'blocks') AND " + blockerActive + ")")
	}
	for name, requested := range map[string]*time.Time{
		"created_at": opts.AfterCreatedAt,
		"updated_at": opts.AfterUpdatedAt,
		"closed_at":  opts.AfterClosedAt,
	} {
		if requested != nil {
			add(sqliteListTimestampExpr("i."+name)+" > ?", canonicalListTimestamp(*requested))
		}
	}

	filterFingerprint := listFilterFingerprint(opts)
	if opts.Cursor != "" {
		cursor, cursorErr := decodeListCursor(opts.Cursor)
		if cursorErr != nil {
			return ListPage{}, cursorErr
		}
		if cursor.Sort != sortKey {
			return ListPage{}, fmt.Errorf("cursor was created for %s, but this request sorts by %s", cursor.Sort, sortKey)
		}
		if cursor.Filter != filterFingerprint {
			return ListPage{}, errors.New("cursor does not match this list's filters or context")
		}
		field := sortField(sortKey)
		timeExpr := sqliteListTimestampExpr("i." + field)
		if cursor.Null {
			add("i."+field+" IS NULL AND i.id > ?", cursor.ID)
		} else {
			add("((i."+field+" IS NOT NULL AND ("+timeExpr+" < ? OR ("+timeExpr+" = ? AND i.id > ?))) OR i."+field+" IS NULL)", cursor.Value, cursor.Value, cursor.ID)
		}
	}

	descriptionExpr := "''"
	if columns["description"] {
		descriptionExpr = "COALESCE(i.description, '')"
	}
	assigneeExpr := "''"
	if columns["assignee"] {
		assigneeExpr = "COALESCE(i.assignee, '')"
	}
	priorityExpr := "0"
	if columns["priority"] {
		priorityExpr = "COALESCE(i.priority, 0)"
	}
	typeExpr := "'task'"
	if columns["issue_type"] {
		typeExpr = "COALESCE(i.issue_type, 'task')"
	}
	createdExpr := "NULL"
	if columns["created_at"] {
		createdExpr = "i.created_at"
	}
	updatedExpr := "NULL"
	if columns["updated_at"] {
		updatedExpr = "i.updated_at"
	}
	closedExpr := "NULL"
	if columns["closed_at"] {
		closedExpr = "i.closed_at"
	}
	labelsExpr := "'[]'"
	if labelsTable {
		labelsExpr = "COALESCE((SELECT json_group_array(label) FROM (SELECT label FROM labels WHERE issue_id = i.id ORDER BY label)), '[]')"
	} else if labelsColumn {
		labelsExpr = "COALESCE(i.labels, '[]')"
	}

	query := "SELECT i.id, i.title, " + descriptionExpr + ", i.status, " + priorityExpr + ", " + typeExpr + ", " + assigneeExpr + ", " + createdExpr + ", " + updatedExpr + ", " + closedExpr + ", " + labelsExpr + " FROM issues i WHERE " + strings.Join(where, " AND ")
	field := sortField(sortKey)
	orderExpr := sqliteListTimestampExpr("i." + field)
	query += " ORDER BY i." + field + " IS NULL ASC, " + orderExpr + " DESC, i.id ASC"
	if opts.Limit > 0 {
		query += " LIMIT ?"
		limit := opts.Limit
		if paginated {
			limit++
		}
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return ListPage{}, fmt.Errorf("querying Hub issues: %w", err)
	}
	defer rows.Close()

	result := make([]listRow, 0)
	for rows.Next() {
		var row listRow
		var description, assignee, created, updated, closed, labels sql.NullString
		if err := rows.Scan(&row.issue.ID, &row.issue.Title, &description, &row.issue.Status, &row.issue.Priority, &row.issue.IssueType, &assignee, &created, &updated, &closed, &labels); err != nil {
			return ListPage{}, fmt.Errorf("reading Hub issue row: %w", err)
		}
		row.issue.Description = description.String
		row.issue.Assignee = assignee.String
		row.issue.CreatedAt = normalizeListTimestamp(created.String)
		row.issue.UpdatedAt = normalizeListTimestamp(updated.String)
		if closed.Valid {
			value := normalizeListTimestamp(closed.String)
			row.issue.ClosedAt = &value
		}
		if labels.Valid && labels.String != "" && labels.String != "null" {
			_ = json.Unmarshal([]byte(labels.String), &row.issue.Labels)
		}
		if opts.Brief {
			row.issue.Description = ""
			row.issue.Assignee = ""
			row.issue.Labels = nil
			row.issue.CreatedAt = ""
			row.issue.ClosedAt = nil
		}
		var orderValue sql.NullString
		switch field {
		case "closed_at":
			orderValue = closed
		case "created_at":
			orderValue = created
		default:
			orderValue = updated
		}
		if orderValue.Valid {
			row.orderTime = canonicalStoredListTimestamp(orderValue.String)
		} else {
			row.orderNull = true
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return ListPage{}, fmt.Errorf("iterating Hub issue rows: %w", err)
	}

	page := ListPage{}
	if paginated && opts.Limit > 0 && len(result) > opts.Limit {
		page.Pagination = &ListPagination{Limit: opts.Limit, HasMore: true}
		result = result[:opts.Limit]
	}
	if paginated && page.Pagination == nil {
		page.Pagination = &ListPagination{Limit: opts.Limit}
	}
	page.Issues = make([]ListIssue, len(result))
	for index, row := range result {
		page.Issues[index] = row.issue
	}
	if page.Pagination != nil && len(result) > 0 {
		last := result[len(result)-1]
		if page.Pagination.HasMore {
			page.Pagination.NextCursor, err = encodeListCursor(listCursor{Version: 1, Sort: sortKey, Value: last.orderTime, Null: last.orderNull, ID: last.issue.ID, Filter: filterFingerprint})
			if err != nil {
				return ListPage{}, fmt.Errorf("encoding next cursor: %w", err)
			}
		}
	}
	return page, nil
}

func normalizeListSort(value string) (string, error) {
	if value == "" {
		return "updated_at:desc", nil
	}
	parts := strings.Split(value, ":")
	if len(parts) != 2 || parts[1] != "desc" || !oneOfList(parts[0], "created_at", "updated_at", "closed_at") {
		return "", fmt.Errorf("invalid sort %q; use created_at:desc, updated_at:desc, or closed_at:desc", value)
	}
	return value, nil
}

func sortField(sortKey string) string { return strings.SplitN(sortKey, ":", 2)[0] }

func oneOfList(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func sqliteReadOnlyDSN(path string) string {
	return (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String()
}

// sqliteListTimestampExpr turns production's UTC RFC3339 text into a fixed
// nine-digit fractional form. SQLite julianday truncates sub-millisecond
// precision, so text keys preserve strict nanosecond boundaries and ties.
func sqliteListTimestampExpr(column string) string {
	return "(substr(" + column + ", 1, 19) || '.' || substr((CASE WHEN substr(" + column + ", 20, 1) = '.' THEN substr(" + column + ", 21, length(" + column + ") - 21) ELSE '' END || '000000000'), 1, 9) || 'Z')"
}

func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, fmt.Errorf("reading Hub database schema: %w", err)
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, fmt.Errorf("reading Hub database schema row: %w", err)
		}
		columns[strings.ToLower(name)] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading Hub database schema: %w", err)
	}
	return columns, nil
}

func hasTable(db *sql.DB, table string) (bool, error) {
	var name string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking Hub database table %q: %w", table, err)
	}
	return true, nil
}

func labelExistsClause(labelsColumn, labelsTable bool) string {
	if labelsTable {
		return "EXISTS (SELECT 1 FROM labels l WHERE l.issue_id = i.id AND l.label = ?)"
	}
	if labelsColumn {
		return "EXISTS (SELECT 1 FROM json_each(COALESCE(i.labels, '[]')) WHERE value = ?)"
	}
	return "0 = 1"
}

func listFilterFingerprint(opts ListOptions) string {
	type fingerprint struct {
		Context        string
		AllContexts    bool
		Statuses       []string
		IssueType      string
		Priority       *int
		Labels         []string
		Ready          bool
		AfterCreatedAt string
		AfterUpdatedAt string
		AfterClosedAt  string
	}
	value := fingerprint{
		Context: opts.Context, AllContexts: opts.AllContexts, Statuses: append([]string(nil), opts.Statuses...),
		IssueType: opts.IssueType, Priority: opts.Priority, Labels: append([]string(nil), opts.Labels...), Ready: opts.Ready,
	}
	if opts.AfterCreatedAt != nil {
		value.AfterCreatedAt = opts.AfterCreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if opts.AfterUpdatedAt != nil {
		value.AfterUpdatedAt = opts.AfterUpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	if opts.AfterClosedAt != nil {
		value.AfterClosedAt = opts.AfterClosedAt.UTC().Format(time.RFC3339Nano)
	}
	data, _ := json.Marshal(value)
	digest := sha256.Sum256(data)
	return fmt.Sprintf("%x", digest[:])
}

func encodeListCursor(cursor listCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeListCursor(value string) (listCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return listCursor{}, errors.New("invalid cursor: cursor is not a valid opaque token")
	}
	var cursor listCursor
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.Version != 1 || cursor.ID == "" || cursor.Filter == "" {
		return listCursor{}, errors.New("invalid cursor: unsupported or malformed cursor token")
	}
	return cursor, nil
}

func normalizeListTimestamp(value string) string {
	if value == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	return value
}

func canonicalListTimestamp(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func canonicalStoredListTimestamp(value string) string {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return canonicalListTimestamp(parsed)
		}
	}
	return value
}
