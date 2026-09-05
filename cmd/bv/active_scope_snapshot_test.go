package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func writeFakeWBD(t *testing.T, output, calls string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wbd")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$WBD_CALLS\"\nif [ \"$2\" = active ]; then printf '%s' \"$WBD_ACTIVE\"; else printf '%s' \"$WBD_SHOW\"; fi\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(path)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WBD_CALLS", calls)
	t.Setenv("WBD_ACTIVE", output)
	t.Setenv("WBD_SHOW", `{"id":"scope-a","members":[{"id":"A"},{"issue_id":"B"}]}`)
}

func TestHubScopeMemberLoaderUsesOnlyPublicScopeCommands(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	writeFakeWBD(t, `{"id":"scope-a"}`, calls)
	loader := newHubScopeMemberLoader(t.TempDir())
	got, err := loader(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Fatalf("scope members = %#v, want [A B]", got)
	}
	callsData, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if string(callsData) != "scope active --json\nscope show scope-a --json\n" {
		t.Fatalf("wbd calls = %q", callsData)
	}
}

func TestHubScopeMemberLoaderTreatsAbsentActiveScopeAsEmpty(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	path := filepath.Join(t.TempDir(), "wbd")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$WBD_CALLS\"\nprintf '%s' 'no active scope' >&2\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(path)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WBD_CALLS", calls)
	got, err := newHubScopeMemberLoader(t.TempDir())(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("absent active scope members = %#v, want nil", got)
	}
}

func TestFilterHubScopeIssuesIsMembershipOnly(t *testing.T) {
	issues := []model.Issue{
		{ID: "A", Labels: []string{"ctx:alpha"}},
		{ID: "B", Labels: []string{"ctx:beta"}},
		{ID: "C", Labels: []string{"ctx:alpha"}},
	}
	got := filterHubScopeIssues(issues, []string{"C", "C"})
	if len(got) != 1 || got[0].ID != "C" {
		t.Fatalf("filtered issues = %#v, want only explicit member C", got)
	}
}
