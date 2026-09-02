package repository

import "testing"

func TestCatalogSortAndSelectionReconciliation(t *testing.T) {
	catalog := Catalog{{ID: "ctx:z", Name: "same"}, {ID: "ctx:b", Name: "alpha"}, {ID: "ctx:a", Name: "same"}}
	SortCatalog(catalog)
	if got := []string{catalog[0].ID, catalog[1].ID, catalog[2].ID}; got[0] != "ctx:b" || got[1] != "ctx:a" || got[2] != "ctx:z" {
		t.Fatalf("sorted catalog IDs = %v", got)
	}
	if got := ReconcileSelection(nil, catalog); got != nil {
		t.Fatalf("nil selection = %#v", got)
	}
	got := ReconcileSelection(map[string]bool{"ctx:a": true, "ctx:gone": true}, catalog)
	if len(got) != 1 || !got["ctx:a"] {
		t.Fatalf("reconciled selection = %#v", got)
	}
	if got := ReconcileSelection(map[string]bool{"ctx:gone": true}, catalog); got != nil {
		t.Fatalf("empty reconciled selection = %#v, want nil", got)
	}
}
