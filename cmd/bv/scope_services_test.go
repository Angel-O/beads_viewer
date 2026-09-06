package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/ui"
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

func TestHubScopeServiceDeactivatesActiveScope(t *testing.T) {
	root := t.TempDir()
	calls := filepath.Join(root, "calls")
	wbd := filepath.Join(root, "wbd")
	if err := os.WriteFile(wbd, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$WBD_SCOPE_CALLS\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WBD_SCOPE_CALLS", calls)

	if err := newHubScopeServices(root).Deactivate(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := splitLines(string(data)), []string{"scope", "deactivate", "--json"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("wbd deactivate args=%#v, want %#v", got, want)
	}
}

func TestHubScopeServicesWireTypedBacklogDetailsAndMutations(t *testing.T) {
	root := t.TempDir()
	calls := filepath.Join(root, "calls")
	wbd := filepath.Join(root, "wbd")
	script := `#!/bin/sh
if [ "$1" = "backlog" ]; then
  printf '%s\n' "$@" > "$WBD_SCOPE_CALLS"
  printf '%s' '{"issues":[{"id":"b1","title":"Backlog","status":"open","issue_type":"task"}],"pagination":{"has_more":true,"next_cursor":"opaque-next"}}'
  exit 0
fi
if [ "$1" = "scope" ] && [ "$2" = "show" ]; then
  printf '%s' '{"id":"today","name":"Today","member_count":1,"members":[{"id":"b1","title":"Member","status":"open","issue_type":"task"}]}'
  exit 0
fi
printf '%s\n' "$@" > "$WBD_SCOPE_CALLS"
printf '%s' '{}'
`
	if err := os.WriteFile(wbd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WBD_SCOPE_CALLS", calls)
	service := newHubScopeServices(root)

	page, err := service.QueryBacklog(context.Background(), ui.BacklogQuery{Filter: " alpha ", Cursor: "opaque/value==", Limit: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Issues) != 1 || !page.HasMore || page.NextCursor != "opaque-next" {
		t.Fatalf("typed backlog page = %#v", page)
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := splitLines(string(data)), []string{"backlog", "list", "--filter", " alpha ", "--limit", "7", "--cursor", "opaque/value==", "--json"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("wbd backlog args=%#v, want %#v", got, want)
	}
	if _, err := service.QueryBacklog(context.Background(), ui.BacklogQuery{Filter: " \t", Limit: 7}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := splitLines(string(data)), []string{"backlog", "list", "--limit", "7", "--json"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("whitespace filter backlog args=%#v, want %#v", got, want)
	}
	details, err := service.LoadDetails(context.Background(), "today")
	if err != nil {
		t.Fatal(err)
	}
	if details.Info.ID != "today" || len(details.Issues) != 1 || details.Issues[0].ID != "b1" || len(details.MemberIDs) != 1 {
		t.Fatalf("typed scope details = %#v", details)
	}

	for _, test := range []struct {
		name     string
		mutation ui.ScopeMutation
		want     []string
	}{
		{name: "batch add", mutation: ui.ScopeMutation{Kind: ui.ScopeMutationAdd, ScopeID: "today", IssueIDs: []string{"b1", "b2"}}, want: []string{"scope", "add", "b1", "b2", "--scope", "today", "--json"}},
		{name: "semantic remove", mutation: ui.ScopeMutation{Kind: ui.ScopeMutationRemove, ScopeID: "today", Label: "team"}, want: []string{"scope", "remove", "--label", "team", "--scope", "today", "--json"}},
		{name: "move", mutation: ui.ScopeMutation{Kind: ui.ScopeMutationMove, IssueIDs: []string{"b1", "b2"}, SourceScopeID: "today", TargetScopeID: "later"}, want: []string{"scope", "move", "b1", "b2", "--source-scope", "today", "--target-scope", "later", "--json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var mutate func(context.Context, ui.ScopeMutation) error
			if test.mutation.Label != "" {
				mutate = service.MutateMatching
			} else {
				mutate = service.Mutate
			}
			if err := mutate(context.Background(), test.mutation); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			if got := splitLines(string(data)); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("wbd mutation args=%#v, want %#v", got, test.want)
			}
		})
	}
}

func splitLines(value string) []string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	return lines
}
