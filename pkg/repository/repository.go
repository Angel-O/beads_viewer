// Package repository contains neutral repository catalog and identity metadata.
package repository

import (
	"sort"
)

// IdentityKind distinguishes exact repository identities from legacy prefixes.
type IdentityKind string

const (
	IdentityExact  IdentityKind = "exact"
	IdentityPrefix IdentityKind = "prefix"
)

// CatalogEntry describes one repository available to a presentation layer.
// ID is the exact identity used for matching; Name is presentation metadata.
type CatalogEntry struct {
	ID        string
	Name      string
	Path      string
	Detail    string
	BeadCount int
	Kind      IdentityKind
}

// Catalog is sorted deterministically by friendly name, then ID.
type Catalog []CatalogEntry

// ReconcileSelection intersects an explicit selection with the current catalog.
// Nil means all repositories, including future additions. If removals empty an
// explicit selection, the result normalizes to all.
func ReconcileSelection(selected map[string]bool, catalog Catalog) map[string]bool {
	if selected == nil {
		return nil
	}
	available := make(map[string]bool, len(catalog))
	for _, entry := range catalog {
		available[entry.ID] = true
	}
	reconciled := make(map[string]bool, len(selected))
	for id, enabled := range selected {
		if enabled && available[id] {
			reconciled[id] = true
		}
	}
	if len(reconciled) == 0 {
		return nil
	}
	return reconciled
}

// SortCatalog applies the catalog's stable display order in place.
func SortCatalog(catalog Catalog) {
	sort.Slice(catalog, func(i, j int) bool {
		if catalog[i].Name != catalog[j].Name {
			return catalog[i].Name < catalog[j].Name
		}
		return catalog[i].ID < catalog[j].ID
	})
}
