package hub

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeListResponsePreservesOpaqueCursorAndProjectsFullIssueShape(t *testing.T) {
	data, err := DecodeListResponse([]byte(`{"issues":[{"id":"one","title":"One","description":"Details","status":"closed","priority":1,"issue_type":"bug","assignee":"agent","labels":["team"],"created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-02T00:00:00.123456789Z","closed_at":"2026-08-03T02:00:00+02:00","extra":{"drop":true}}],"pagination":{"limit":1,"has_more":true,"next_cursor":"opaque:/+= token"}}`), true, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Issues     []ListIssue `json:"issues"`
		Pagination struct {
			Limit      int    `json:"limit"`
			HasMore    bool   `json:"has_more"`
			NextCursor string `json:"next_cursor"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	if response.Pagination.NextCursor != "opaque:/+= token" {
		t.Fatalf("response = %s", data)
	}
	want := `{"issues":[{"id":"one","title":"One","description":"Details","status":"closed","priority":1,"issue_type":"bug","assignee":"agent","labels":["team"],"created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-02T00:00:00.123456789Z","closed_at":"2026-08-03T02:00:00+02:00"}],"pagination":{"limit":1,"has_more":true,"next_cursor":"opaque:/+= token"}}` + "\n"
	if string(data) != want {
		t.Fatalf("full response = %s, want %s", data, want)
	}
}

func TestDecodeListResponseBriefProjectionIsFixed(t *testing.T) {
	data, err := DecodeListResponse([]byte(`[{"id":"one","title":"One","status":"open","priority":2,"issue_type":"task","updated_at":"2026-08-28T00:00:00Z","description":"drop","labels":["drop"]}]`), false, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"id":"one","title":"One","status":"open","priority":2,"issue_type":"task","updated_at":"2026-08-28T00:00:00Z"}]` + "\n"
	if string(data) != want {
		t.Fatalf("brief response = %s, want %s", data, want)
	}
}

func TestDecodeListResponseRejectsMissingRequiredFields(t *testing.T) {
	validFull := map[string]any{
		"id": "one", "title": "One", "status": "open", "priority": 0,
		"issue_type": "task", "created_at": "2026-08-01T00:00:00Z",
		"updated_at": "2026-08-02T00:00:00Z",
	}
	for _, testCase := range []struct {
		name  string
		brief bool
		field string
	}{
		{name: "full missing id", field: "id"},
		{name: "full missing title", field: "title"},
		{name: "full missing status", field: "status"},
		{name: "full missing priority", field: "priority"},
		{name: "full missing issue type", field: "issue_type"},
		{name: "full missing created timestamp", field: "created_at"},
		{name: "full missing updated timestamp", field: "updated_at"},
		{name: "brief missing id", brief: true, field: "id"},
		{name: "brief missing title", brief: true, field: "title"},
		{name: "brief missing status", brief: true, field: "status"},
		{name: "brief missing priority", brief: true, field: "priority"},
		{name: "brief missing issue type", brief: true, field: "issue_type"},
		{name: "brief missing updated timestamp", brief: true, field: "updated_at"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			row := make(map[string]any, len(validFull))
			for key, value := range validFull {
				row[key] = value
			}
			delete(row, testCase.field)
			data, err := json.Marshal([]any{row})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeListResponse(data, false, 0, testCase.brief); err == nil || !strings.Contains(err.Error(), testCase.field) {
				t.Fatalf("DecodeListResponse missing %s error = %v", testCase.field, err)
			}
		})
	}
}

func TestDecodeListResponseDistinguishesZeroPriorityAndOwnerAssigneeDrift(t *testing.T) {
	data, err := DecodeListResponse([]byte(`[{"id":"one","title":"One","status":"open","priority":0,"issue_type":"task","created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-02T00:00:00Z","owner":"team-owner"}]`), false, 0, false)
	if err != nil {
		t.Fatalf("priority zero with owner-only row rejected: %v", err)
	}
	want := `[{"id":"one","title":"One","description":"","status":"open","priority":0,"issue_type":"task","assignee":"","labels":[],"created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-02T00:00:00Z","closed_at":null}]` + "\n"
	if string(data) != want {
		t.Fatalf("owner-only full response = %s, want %s", data, want)
	}

	missingPriority := []byte(`[{"id":"one","title":"One","status":"open","issue_type":"task","created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-02T00:00:00Z"}]`)
	if _, err := DecodeListResponse(missingPriority, false, 0, false); err == nil || !strings.Contains(err.Error(), `"priority"`) {
		t.Fatalf("missing priority error = %v", err)
	}
}

func TestDecodeListResponseRejectsMalformedEnvelopes(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "array for page", data: `[]`},
		{name: "missing pagination", data: `{"issues":[]}`},
		{name: "null issues", data: `{"issues":null,"pagination":{"limit":1,"has_more":false}}`},
		{name: "missing metadata", data: `{"issues":[],"pagination":{}}`},
		{name: "wrong limit", data: `{"issues":[],"pagination":{"limit":2,"has_more":false}}`},
		{name: "missing cursor", data: `{"issues":[],"pagination":{"limit":1,"has_more":true}}`},
		{name: "terminal cursor", data: `{"issues":[],"pagination":{"limit":1,"has_more":false,"next_cursor":"unexpected"}}`},
		{name: "non-object issue", data: `{"issues":[1],"pagination":{"limit":1,"has_more":false}}`},
		{name: "trailing output", data: `{"issues":[],"pagination":{"limit":1,"has_more":false}} noise`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := DecodeListResponse([]byte(testCase.data), true, 1, false); err == nil {
				t.Fatalf("DecodeListResponse(%s) succeeded", testCase.data)
			}
		})
	}
}

func TestBackendListSort(t *testing.T) {
	for input, want := range map[string]string{"created_at:desc": "created", "updated_at:desc": "updated", "closed_at:desc": "closed", "created-desc": "created", "priority-asc": "priority"} {
		if got := BackendListSort(input); got != want {
			t.Errorf("BackendListSort(%q) = %q, want %q", input, got, want)
		}
	}
	if strings.Contains(BackendListSort("updated_at:desc"), ":") {
		t.Fatal("backend sort retained wbd syntax")
	}
}
