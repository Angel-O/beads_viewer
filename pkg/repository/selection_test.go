package repository

import (
	"reflect"
	"testing"
)

func TestSelectionNormalizesAndDefensivelyCopiesIDs(t *testing.T) {
	ids := []string{"repo:b", "repo:a", "repo:b"}
	selection, err := NewSelectedAndUnassignedSelection(ids)
	if err != nil {
		t.Fatal(err)
	}
	ids[0] = "changed"
	if got := selection.IDs(); !reflect.DeepEqual(got, []string{"repo:a", "repo:b"}) {
		t.Fatalf("normalized IDs = %v", got)
	}
	got := selection.IDs()
	got[0] = "changed"
	if selection.IDs()[0] != "repo:a" {
		t.Fatal("IDs accessor exposed internal storage")
	}
	if !selection.Matches([]string{"repo:b"}) || !selection.Matches(nil) || selection.Matches([]string{"repo:c"}) {
		t.Fatal("selected-plus-unassigned matching is incorrect")
	}

	if !NewAllSelection().Matches([]string{"anything"}) || !NewUnassignedSelection().Matches(nil) || NewUnassignedSelection().Matches([]string{"repo:a"}) {
		t.Fatal("all/unassigned matching is incorrect")
	}
	if _, err := NewSelectedSelection(nil); err == nil {
		t.Fatal("empty selected IDs were accepted")
	}
}

func TestSelectionReconcileKeepsVariant(t *testing.T) {
	selection, err := NewSelectedAndUnassignedSelection([]string{"gone", "keep"})
	if err != nil {
		t.Fatal(err)
	}
	got := selection.Reconcile(Catalog{{ID: "keep"}, {ID: "other"}})
	if got.Mode() != SelectionSelected || !got.IncludesUnassigned() || !reflect.DeepEqual(got.IDs(), []string{"keep"}) {
		t.Fatalf("reconciled selection = %#v", got)
	}
	if got = selection.Reconcile(nil); got.Mode() != SelectionUnassigned {
		t.Fatalf("empty reconciled selection = %#v, want unassigned", got)
	}
}
