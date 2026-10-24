package main

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParseUpdateContextReplacement(t *testing.T) {
	request, err := parse([]string{"update", "item-1", "--context", "ctx:a", "--context", "ctx:b", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(request.updateContexts, []string{"ctx:a", "ctx:b"}) || !request.json {
		t.Fatalf("request = %#v", request)
	}
	request, err = parse([]string{"update", "todo-1", "--contextless"})
	if err != nil || !request.updateContextless {
		t.Fatalf("request=%#v err=%v", request, err)
	}
	for _, args := range [][]string{
		{"update", "item-1", "--context", "ctx:a", "--contextless"},
		{"update", "item-1", "--contextless", "--context", "ctx:a"},
	} {
		if _, err := parse(args); err == nil {
			t.Fatalf("accepted conflicting flags: %v", args)
		}
	}
}

func TestUpdateContextPreflightAndBackendForwarding(t *testing.T) {
	test := newAppTest(t, true)
	writeHubConfig(t, test, map[string]string{"ctx:target": test.repository})
	setResponses(t, map[string]string{"show:item-1": `[{"id":"item-1","issue_type":"task","status":"open","labels":["ctx:old","team"]}]`})
	code, _, stderr := test.run("update", "item-1", "--context", "ctx:target", "--json")
	if code != 0 {
		t.Fatalf("update failed: %s", stderr)
	}
	calls := test.calls()
	if len(calls) != 2 || !reflect.DeepEqual(calls[1].Args, []string{"--db", test.store, "--json", "update", "item-1", "--context", "ctx:target"}) {
		t.Fatalf("backend calls = %#v", calls)
	}
	if err := os.WriteFile(test.app.paths.Ledger, []byte(`{"bead_id":"item-1","context":"ctx:old","commit":"`+strings.Repeat("a", 40)+`"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = test.run("update", "item-1", "--context", "ctx:target")
	if code == 0 || !strings.Contains(stderr, "pre-work only") {
		t.Fatalf("history-bearing update: code=%d stderr=%s", code, stderr)
	}
	calls = test.calls()
	if len(calls) != 3 || calls[2].Args[3] != "show" {
		t.Fatalf("history preflight calls = %d, final command = %v", len(calls), calls[len(calls)-1].Args)
	}
}
