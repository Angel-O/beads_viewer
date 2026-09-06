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

	page, err := service.QueryBacklog(context.Background(), ui.BacklogQuery{Filter: " alpha ", Label: "team", Status: "blocked", Cursor: "opaque/value==", Limit: 7})
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
	if got, want := splitLines(string(data)), []string{"backlog", "list", "--label", "team", "--filter", " alpha ", "--status", "blocked", "--limit", "7", "--cursor", "opaque/value==", "--json"}; !reflect.DeepEqual(got, want) {
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

func TestHubScopeServiceForwardsBacklogContextSelection(t *testing.T) {
	root := t.TempDir()
	calls := filepath.Join(root, "calls")
	wbd := filepath.Join(root, "wbd")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$WBD_SCOPE_CALLS"
printf '%s' '{"issues":[],"pagination":{}}'
`
	if err := os.WriteFile(wbd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WBD_SCOPE_CALLS", calls)
	service := newHubScopeServices(root)

	for _, test := range []struct {
		name  string
		query ui.BacklogQuery
		want  []string
	}{
		{name: "all/no selection", query: ui.BacklogQuery{Limit: 7}, want: []string{"backlog", "list", "--limit", "7", "--json"}},
		{name: "single", query: ui.BacklogQuery{Contexts: []string{"ctx:one"}, Limit: 7}, want: []string{"backlog", "list", "--context", "ctx:one", "--limit", "7", "--json"}},
		{name: "ordered multiple", query: ui.BacklogQuery{Contexts: []string{"ctx:two", "ctx:one", "ctx:three"}, Limit: 7}, want: []string{"backlog", "list", "--context", "ctx:two", "--context", "ctx:one", "--context", "ctx:three", "--limit", "7", "--json"}},
		{name: "contextless composition", query: ui.BacklogQuery{Contexts: []string{"ctx:one", "ctx:two"}, IncludeContextless: true, Limit: 7}, want: []string{"backlog", "list", "--context", "ctx:one", "--context", "ctx:two", "--contextless", "--limit", "7", "--json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.QueryBacklog(context.Background(), test.query); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			if got := splitLines(string(data)); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("wbd backlog args=%#v, want %#v", got, test.want)
			}
		})
	}
}

func TestHubScopeServicesQueryCatalogAndMembersBuildPagedCommands(t *testing.T) {
	root := t.TempDir()
	calls := filepath.Join(root, "calls")
	wbd := filepath.Join(root, "wbd")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$WBD_SCOPE_CALLS"
if [ "$2" = "list" ]; then
  printf '%s' '{"schema_version":1,"items":[{"id":"scope-a","name":"Today","member_count":2,"new_scope_field":"allowed"}],"limit":2,"returned_count":1,"total_matching":3,"has_more":true,"next_cursor":"catalog-next"}'
else
  printf '%s' '{"schema_version":1,"scope":{"id":"scope-a","name":"Today","member_count":99,"created_on":"2026-09-05T00:00:00Z","new_scope_field":"allowed"},"members":[{"id":"b1","title":"Member","status":"open","issue_type":"task","new_member_field":"allowed"}],"member_count":2,"completed_count":1,"limit":3,"returned_count":1,"total_matching":1,"has_more":false}'
fi
`
	if err := os.WriteFile(wbd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WBD_SCOPE_CALLS", calls)
	service := newHubScopeServices(root)

	catalog, err := service.QueryCatalog(context.Background(), ui.ScopeCatalogQuery{Limit: 2, Cursor: "catalog-cursor"})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Scopes) != 1 || catalog.Scopes[0].ID != "scope-a" || !catalog.HasMore || catalog.NextCursor != "catalog-next" {
		t.Fatalf("catalog page = %#v", catalog)
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := splitLines(string(data)), []string{"scope", "list", "--paginate", "--limit", "2", "--cursor", "catalog-cursor", "--json"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog wbd args=%#v, want %#v", got, want)
	}

	members, err := service.QueryMembers(context.Background(), ui.ScopeMembersQuery{
		ScopeID: "scope-a", Limit: 3, Cursor: "member-cursor", Status: "completed", Type: "task", Contexts: []string{"ctx:a", "ctx:b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if members.Scope.ID != "scope-a" || members.Scope.Name != "Today" || members.Scope.MemberCount != 2 || !members.Scope.CreatedAt.Equal(time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)) || len(members.Members) != 1 || members.Members[0].ID != "b1" || members.HasMore || members.NextCursor != "" {
		t.Fatalf("members page = %#v", members)
	}
	data, err = os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := splitLines(string(data)), []string{
		"scope", "show", "scope-a", "--paginate", "--limit", "3", "--cursor", "member-cursor",
		"--status", "completed", "--type", "task", "--context", "ctx:a", "--context", "ctx:b", "--json",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("members wbd args=%#v, want %#v", got, want)
	}
}

func TestDecodePagedScopeEnvelopesRejectsMalformedMetadata(t *testing.T) {
	tests := []struct {
		name   string
		data   string
		decode func([]byte) error
	}{
		{name: "catalog unknown envelope field", data: `{"items":[],"limit":2,"returned_count":0,"total_matching":0,"has_more":false,"extra":true}`, decode: func(data []byte) error {
			_, err := decodeScopeCatalogPage(data, 2)
			return err
		}},
		{name: "catalog unknown metadata field", data: `{"items":[],"limit":2,"returned_count":0,"total_matching":0,"has_more":false,"metadata":true}`, decode: func(data []byte) error {
			_, err := decodeScopeCatalogPage(data, 2)
			return err
		}},
		{name: "catalog missing metadata field", data: `{"items":[],"limit":2,"returned_count":0,"has_more":false}`, decode: func(data []byte) error {
			_, err := decodeScopeCatalogPage(data, 2)
			return err
		}},
		{name: "catalog limit mismatch", data: `{"items":[],"limit":3,"returned_count":0,"total_matching":0,"has_more":false}`, decode: func(data []byte) error {
			_, err := decodeScopeCatalogPage(data, 2)
			return err
		}},
		{name: "catalog missing next cursor", data: `{"items":[],"limit":2,"returned_count":0,"total_matching":1,"has_more":true}`, decode: func(data []byte) error {
			_, err := decodeScopeCatalogPage(data, 2)
			return err
		}},
		{name: "catalog terminal cursor", data: `{"items":[],"limit":2,"returned_count":0,"total_matching":0,"has_more":false,"next_cursor":"stale"}`, decode: func(data []byte) error {
			_, err := decodeScopeCatalogPage(data, 2)
			return err
		}},
		{name: "catalog returned count mismatch", data: `{"items":[],"limit":2,"returned_count":1,"total_matching":1,"has_more":false}`, decode: func(data []byte) error {
			_, err := decodeScopeCatalogPage(data, 2)
			return err
		}},
		{name: "members unknown envelope field", data: `{"scope":{},"members":[],"member_count":0,"completed_count":0,"limit":2,"returned_count":0,"total_matching":0,"has_more":false,"extra":true}`, decode: func(data []byte) error {
			_, err := decodeScopeMembersPage(data, 2, "scope-a")
			return err
		}},
		{name: "members malformed array", data: `{"scope":{},"members":{},"member_count":0,"completed_count":0,"limit":2,"returned_count":0,"total_matching":0,"has_more":false}`, decode: func(data []byte) error {
			_, err := decodeScopeMembersPage(data, 2, "scope-a")
			return err
		}},
		{name: "members missing field", data: `{"scope":{},"member_count":0,"completed_count":0,"limit":2,"returned_count":0,"total_matching":0,"has_more":false}`, decode: func(data []byte) error {
			_, err := decodeScopeMembersPage(data, 2, "scope-a")
			return err
		}},
		{name: "members returned count mismatch", data: `{"scope":{},"members":[],"member_count":0,"completed_count":0,"limit":2,"returned_count":1,"total_matching":1,"has_more":false}`, decode: func(data []byte) error {
			_, err := decodeScopeMembersPage(data, 2, "scope-a")
			return err
		}},
		{name: "members missing required counts", data: `{"scope":{},"members":[],"limit":2,"returned_count":0,"total_matching":0,"has_more":false}`, decode: func(data []byte) error {
			_, err := decodeScopeMembersPage(data, 2, "scope-a")
			return err
		}},
		{name: "members completed count exceeds total", data: `{"scope":{},"members":null,"member_count":1,"completed_count":2,"limit":2,"returned_count":0,"total_matching":0,"has_more":false}`, decode: func(data []byte) error {
			_, err := decodeScopeMembersPage(data, 2, "scope-a")
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.decode([]byte(test.data)); err == nil {
				t.Fatal("malformed envelope unexpectedly decoded")
			}
		})
	}
}

func TestDecodeScopeMembersPageAcceptsNullMembersAndUsesTopLevelCount(t *testing.T) {
	page, err := decodeScopeMembersPage([]byte(`{"scope":{"id":"scope-a","name":"Today","member_count":99},"members":null,"member_count":0,"completed_count":0,"limit":2,"returned_count":0,"total_matching":0,"has_more":false}`), 2, "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	if page.Scope.ID != "scope-a" || page.Scope.Name != "Today" || page.Scope.MemberCount != 0 || page.Members == nil || len(page.Members) != 0 {
		t.Fatalf("decoded empty members page = %#v", page)
	}
}

func TestHubScopeServicesKeepLegacyCompleteLoadersUnpaginated(t *testing.T) {
	root := t.TempDir()
	calls := filepath.Join(root, "calls")
	wbd := filepath.Join(root, "wbd")
	script := `#!/bin/sh
printf '%s\n' "$@" >> "$WBD_SCOPE_CALLS"
case "$2" in
list) printf '%s' '{"scopes":[{"id":"scope-a","name":"Today"}]}' ;;
active) printf '%s' '{"id":"scope-a"}' ;;
show) printf '%s' '{"id":"scope-a","members":[{"id":"b1","title":"Member"}]}' ;;
esac
`
	if err := os.WriteFile(wbd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WBD_SCOPE_CALLS", calls)
	service := newHubScopeServices(root)
	if _, err := service.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.LoadDetails(context.Background(), "scope-a"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := splitLines(string(data)), []string{
		"scope", "list", "--json", "scope", "active", "--json", "scope", "show", "scope-a", "--json",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy wbd args=%#v, want %#v", got, want)
	}
}

func splitLines(value string) []string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	return lines
}
