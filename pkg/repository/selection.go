package repository

import (
	"fmt"
	"sort"
)

// SelectionMode identifies the repository identities included by a Selection.
type SelectionMode string

const (
	SelectionAll        SelectionMode = "all"
	SelectionSelected   SelectionMode = "selected"
	SelectionUnassigned SelectionMode = "unassigned"
)

// Selection is the neutral repository projection shared by local and Hub
// presentation. It does not assign meaning to repository ID spelling.
type Selection struct {
	mode              SelectionMode
	ids               []string
	includeUnassigned bool
}

// NewAllSelection selects every repository, including unassigned items.
func NewAllSelection() Selection { return Selection{mode: SelectionAll} }

// NewSelectedSelection selects the supplied repository IDs.
func NewSelectedSelection(ids []string) (Selection, error) {
	return newSelection(SelectionSelected, ids, false)
}

// NewUnassignedSelection selects only items without a repository identity.
func NewUnassignedSelection() Selection { return Selection{mode: SelectionUnassigned} }

// NewSelectedAndUnassignedSelection selects the supplied IDs and unassigned
// items.
func NewSelectedAndUnassignedSelection(ids []string) (Selection, error) {
	return newSelection(SelectionSelected, ids, true)
}

func newSelection(mode SelectionMode, ids []string, includeUnassigned bool) (Selection, error) {
	normalized := append([]string(nil), ids...)
	sort.Strings(normalized)
	unique := normalized[:0]
	for _, id := range normalized {
		if id == "" {
			return Selection{}, fmt.Errorf("repository selection IDs cannot be empty")
		}
		if len(unique) == 0 || unique[len(unique)-1] != id {
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return Selection{}, fmt.Errorf("selected repository IDs cannot be empty")
	}
	return Selection{mode: mode, ids: unique, includeUnassigned: includeUnassigned}, nil
}

// Validate reports whether the selection is one of the supported variants.
func (s Selection) Validate() error {
	switch s.mode {
	case SelectionAll, SelectionUnassigned:
		if len(s.ids) != 0 || s.includeUnassigned {
			return fmt.Errorf("repository selection %q cannot include IDs or unassigned items", s.mode)
		}
		return nil
	case SelectionSelected:
		if len(s.ids) == 0 {
			return fmt.Errorf("selected repository IDs cannot be empty")
		}
		for i, id := range s.ids {
			if id == "" || i > 0 && s.ids[i-1] >= id {
				return fmt.Errorf("selected repository IDs must be sorted and unique")
			}
		}
		return nil
	default:
		return fmt.Errorf("invalid repository selection mode: %q", s.mode)
	}
}

// Mode reports the selection variant.
func (s Selection) Mode() SelectionMode { return s.mode }

// IDs returns a detached, sorted list of selected repository IDs.
func (s Selection) IDs() []string { return append([]string(nil), s.ids...) }

// IncludesUnassigned reports whether unassigned items are included alongside
// selected repository IDs.
func (s Selection) IncludesUnassigned() bool { return s.includeUnassigned }

// Clone returns a detached copy of the selection.
func (s Selection) Clone() Selection {
	s.ids = append([]string(nil), s.ids...)
	return s
}

// Matches reports whether repositoryIDs belong to this selection. An empty
// repositoryIDs slice represents an unassigned item.
func (s Selection) Matches(repositoryIDs []string) bool {
	if s.mode == SelectionAll {
		return true
	}
	if len(repositoryIDs) == 0 {
		return s.mode == SelectionUnassigned || s.includeUnassigned
	}
	if s.mode != SelectionSelected {
		return false
	}
	for _, id := range repositoryIDs {
		index := sort.SearchStrings(s.ids, id)
		if index < len(s.ids) && s.ids[index] == id {
			return true
		}
	}
	return false
}

// Reconcile removes selected IDs absent from catalog while retaining the
// selection variant. A selected set emptied by reconciliation becomes all, or
// unassigned when it included unassigned items.
func (s Selection) Reconcile(catalog Catalog) Selection {
	if s.mode != SelectionSelected {
		return s.Clone()
	}
	available := make(map[string]bool, len(catalog))
	for _, entry := range catalog {
		available[entry.ID] = true
	}
	ids := make([]string, 0, len(s.ids))
	for _, id := range s.ids {
		if available[id] {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		if s.includeUnassigned {
			return NewUnassignedSelection()
		}
		return NewAllSelection()
	}
	result, _ := newSelection(SelectionSelected, ids, s.includeUnassigned)
	if s.includeUnassigned && len(ids) == len(catalog) {
		return NewAllSelection()
	}
	return result
}
