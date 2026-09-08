package updater

import "errors"

// forkBuild marks this source fork's bv build as externally managed. Keep this
// as source-level policy rather than an environment switch: the updater must
// not replace a dotfiles-installed binary by accident.
const forkBuild = true

// ErrSelfUpdateDisabled explains how to update this externally managed build.
var ErrSelfUpdateDisabled = errors.New("self-update is disabled in this fork; update bv through the externally managed dotfiles installation path")

// RejectMutatingUpdate reports whether this build may replace or restore its
// executable. Read-only release checks remain available.
func RejectMutatingUpdate() error {
	if forkBuild {
		return ErrSelfUpdateDisabled
	}
	return nil
}
