package main

import (
	"testing"
	"time"
)

func TestDecodeScopeInfos(t *testing.T) {
	scopes, err := decodeScopeInfos([]byte(`{"scopes":[{"id":"s1","name":"Today","member_count":4,"created_at":"2026-09-05T00:00:00Z"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 1 || scopes[0].ID != "s1" || scopes[0].Name != "Today" || scopes[0].MemberCount != 4 {
		t.Fatalf("decoded scopes = %#v", scopes)
	}
	if !scopes[0].CreatedAt.Equal(time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("created_at = %v", scopes[0].CreatedAt)
	}
}

func TestDecodeBacklogPagePreservesOpaqueCursor(t *testing.T) {
	page, err := decodeBacklogPage([]byte(`{"issues":[{"id":"b1","title":"Backlog","status":"open","priority":1,"issue_type":"task","created_at":"2026-09-01T00:00:00Z","updated_at":"2026-09-05T00:00:00Z"}],"pagination":{"has_more":true,"next_cursor":"opaque/value=="}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Issues) != 1 || page.Issues[0].ID != "b1" || page.NextCursor != "opaque/value==" || !page.HasMore {
		t.Fatalf("decoded page = %#v", page)
	}
}
