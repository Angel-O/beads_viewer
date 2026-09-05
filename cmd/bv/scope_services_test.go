package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestHubScopeServiceCreatesSluggedInactiveScopeFromName(t *testing.T) {
	root := t.TempDir()
	calls := filepath.Join(root, "calls")
	wbd := filepath.Join(root, "wbd")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$WBD_SCOPE_CALLS\"\n"
	if err := os.WriteFile(wbd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WBD_SCOPE_CALLS", calls)

	service := newHubScopeServices(root)
	if err := service.Create(context.Background(), "First Release"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := splitLines(string(data)), []string{"scope", "create", "first-release", "First Release", "--json"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("wbd create args=%#v, want %#v", got, want)
	}
}

func splitLines(value string) []string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	return lines
}
