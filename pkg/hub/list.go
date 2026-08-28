package hub

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// ListPagination is the backend-owned pagination metadata returned by bd.
type ListPagination struct {
	Limit      int    `json:"limit"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type listEnvelope struct {
	Issues     json.RawMessage `json:"issues"`
	Pagination json.RawMessage `json:"pagination"`
}

type listPaginationWire struct {
	Limit      *int    `json:"limit"`
	HasMore    *bool   `json:"has_more"`
	NextCursor *string `json:"next_cursor,omitempty"`
}

// ListIssue is the stable full issue projection exposed by wbd list.
type ListIssue struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Priority    int      `json:"priority"`
	IssueType   string   `json:"issue_type"`
	Assignee    string   `json:"assignee"`
	Labels      []string `json:"labels"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	ClosedAt    *string  `json:"closed_at"`
}

type briefListIssue struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Priority  int    `json:"priority"`
	IssueType string `json:"issue_type"`
	UpdatedAt string `json:"updated_at"`
}

type listIssueFields struct {
	ID          *string          `json:"id"`
	Title       *string          `json:"title"`
	Description *string          `json:"description"`
	Status      *string          `json:"status"`
	Priority    *int             `json:"priority"`
	IssueType   *string          `json:"issue_type"`
	Assignee    *string          `json:"assignee"`
	Labels      *[]string        `json:"labels"`
	CreatedAt   *string          `json:"created_at"`
	UpdatedAt   *string          `json:"updated_at"`
	ClosedAt    *json.RawMessage `json:"closed_at"`
}

// BackendListSort maps wbd's public sort spelling to bd's list contract.
func BackendListSort(value string) string {
	return strings.TrimSuffix(value, "_at:desc")
}

// DecodeListResponse validates bd output and applies wbd's stable brief shape.
// It returns bytes only after the entire child response has been accepted.
func DecodeListResponse(data []byte, paginated bool, expectedLimit int, brief bool) ([]byte, error) {
	if paginated {
		var envelope listEnvelope
		if err := decodeSingleJSON(data, &envelope); err != nil {
			return nil, err
		}
		if len(envelope.Issues) == 0 || len(envelope.Pagination) == 0 || bytes.Equal(envelope.Pagination, []byte("null")) {
			return nil, errors.New("paginated response must contain issues and pagination")
		}
		issues, err := decodeIssues(envelope.Issues, brief)
		if err != nil {
			return nil, fmt.Errorf("invalid issues: %w", err)
		}
		var pagination listPaginationWire
		if err := decodeSingleJSON(envelope.Pagination, &pagination); err != nil {
			return nil, fmt.Errorf("invalid pagination: %w", err)
		}
		if pagination.Limit == nil || pagination.HasMore == nil {
			return nil, errors.New("pagination must contain limit and has_more")
		}
		if *pagination.Limit != expectedLimit {
			return nil, fmt.Errorf("pagination limit is %d, want %d", *pagination.Limit, expectedLimit)
		}
		if *pagination.HasMore && (pagination.NextCursor == nil || *pagination.NextCursor == "") {
			return nil, errors.New("pagination with has_more=true must contain next_cursor")
		}
		if !*pagination.HasMore && pagination.NextCursor != nil && *pagination.NextCursor != "" {
			return nil, errors.New("terminal pagination must not contain next_cursor")
		}
		result := struct {
			Issues     any            `json:"issues"`
			Pagination ListPagination `json:"pagination"`
		}{Issues: issues, Pagination: ListPagination{Limit: *pagination.Limit, HasMore: *pagination.HasMore}}
		if pagination.NextCursor != nil {
			result.Pagination.NextCursor = *pagination.NextCursor
		}
		return marshalListResponse(result)
	}

	issues, err := decodeIssues(data, brief)
	if err != nil {
		return nil, err
	}
	return marshalListResponse(issues)
}

func decodeIssues(data []byte, brief bool) (any, error) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, errors.New("issues must be a JSON array")
	}
	var rows []json.RawMessage
	if err := decodeSingleJSON(data, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []json.RawMessage{}
	}
	if brief {
		issues := make([]briefListIssue, len(rows))
		for index, row := range rows {
			fields, err := decodeListIssueFields(row, index, true)
			if err != nil {
				return nil, err
			}
			issues[index] = briefListIssue{ID: *fields.ID, Title: *fields.Title, Status: *fields.Status, Priority: *fields.Priority, IssueType: *fields.IssueType, UpdatedAt: *fields.UpdatedAt}
		}
		return issues, nil
	}
	issues := make([]ListIssue, len(rows))
	for index, row := range rows {
		fields, err := decodeListIssueFields(row, index, false)
		if err != nil {
			return nil, err
		}
		issue := ListIssue{ID: *fields.ID, Title: *fields.Title, Status: *fields.Status, Priority: *fields.Priority, IssueType: *fields.IssueType, CreatedAt: *fields.CreatedAt, UpdatedAt: *fields.UpdatedAt, Labels: []string{}}
		if fields.Description != nil {
			issue.Description = *fields.Description
		}
		if fields.Assignee != nil {
			issue.Assignee = *fields.Assignee
		}
		if fields.Labels != nil {
			issue.Labels = *fields.Labels
		}
		if fields.ClosedAt != nil {
			if !bytes.Equal(bytes.TrimSpace(*fields.ClosedAt), []byte("null")) {
				var closedAt string
				if err := json.Unmarshal(*fields.ClosedAt, &closedAt); err != nil {
					return nil, fmt.Errorf("list row %d field %q must be an RFC3339 timestamp or null: %w", index, "closed_at", err)
				}
				if err := validateListTimestamp(index, "closed_at", closedAt); err != nil {
					return nil, err
				}
				issue.ClosedAt = &closedAt
			}
		}
		issues[index] = issue
	}
	return issues, nil
}

func decodeListIssueFields(data []byte, index int, brief bool) (listIssueFields, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		if err == nil {
			err = errors.New("row must be a JSON object")
		}
		return listIssueFields{}, fmt.Errorf("list row %d must be an object: %w", index, err)
	}
	var result listIssueFields
	var err error
	if result.ID, err = requiredListString(fields, index, "id"); err != nil {
		return result, err
	}
	if result.Title, err = requiredListString(fields, index, "title"); err != nil {
		return result, err
	}
	if result.Status, err = requiredListString(fields, index, "status"); err != nil {
		return result, err
	}
	if result.Priority, err = requiredListInt(fields, index, "priority"); err != nil {
		return result, err
	}
	if result.IssueType, err = requiredListString(fields, index, "issue_type"); err != nil {
		return result, err
	}
	if result.UpdatedAt, err = requiredListTimestamp(fields, index, "updated_at"); err != nil {
		return result, err
	}
	if brief {
		return result, nil
	}
	if result.CreatedAt, err = requiredListTimestamp(fields, index, "created_at"); err != nil {
		return result, err
	}
	result.Description, err = optionalListString(fields, index, "description")
	if err != nil {
		return result, err
	}
	result.Assignee, err = optionalListString(fields, index, "assignee")
	if err != nil {
		return result, err
	}
	if raw, ok := fields["labels"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		var labels []string
		if err := json.Unmarshal(raw, &labels); err != nil {
			return result, fmt.Errorf("list row %d field %q must be an array of strings: %w", index, "labels", err)
		}
		result.Labels = &labels
	}
	if raw, ok := fields["closed_at"]; ok {
		result.ClosedAt = &raw
	}
	return result, nil
}

func requiredListString(fields map[string]json.RawMessage, index int, name string) (*string, error) {
	raw, ok := fields[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("list row %d missing required field %q", index, name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("list row %d field %q must be a non-empty string: %w", index, name, err)
	}
	if value == "" {
		return nil, fmt.Errorf("list row %d field %q must be a non-empty string", index, name)
	}
	return &value, nil
}

func requiredListInt(fields map[string]json.RawMessage, index int, name string) (*int, error) {
	raw, ok := fields[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("list row %d missing required field %q", index, name)
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("list row %d field %q must be an integer: %w", index, name, err)
	}
	return &value, nil
}

func requiredListTimestamp(fields map[string]json.RawMessage, index int, name string) (*string, error) {
	raw, ok := fields[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("list row %d missing required field %q", index, name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("list row %d field %q must be an RFC3339 timestamp: %w", index, name, err)
	}
	if err := validateListTimestamp(index, name, value); err != nil {
		return nil, err
	}
	return &value, nil
}

func validateListTimestamp(index int, name, value string) error {
	if value == "" {
		return fmt.Errorf("list row %d field %q must be an RFC3339 timestamp", index, name)
	}
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return fmt.Errorf("list row %d field %q must be an RFC3339 timestamp: %w", index, name, err)
	}
	return nil
}

func optionalListString(fields map[string]json.RawMessage, index int, name string) (*string, error) {
	raw, ok := fields[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("list row %d optional field %q must be a string: %w", index, name, err)
	}
	return &value, nil
}

func decodeSingleJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("response contains multiple JSON values")
		}
		return err
	}
	return nil
}

func marshalListResponse(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
