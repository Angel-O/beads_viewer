package updater

import (
	"bytes"
	"errors"
	"testing"
)

func TestForkBuildRejectsMutatingUpdateEntryPointsBeforeWork(t *testing.T) {
	if err := RejectMutatingUpdate(); !errors.Is(err, ErrSelfUpdateDisabled) {
		t.Fatalf("RejectMutatingUpdate() = %v, want %v", err, ErrSelfUpdateDisabled)
	}

	var progress bytes.Buffer
	if _, err := PerformUpdate(&Release{TagName: "v99.0.0"}, &progress); !errors.Is(err, ErrSelfUpdateDisabled) {
		t.Fatalf("PerformUpdate() error = %v, want %v", err, ErrSelfUpdateDisabled)
	}
	if progress.Len() != 0 {
		t.Fatalf("PerformUpdate() wrote progress before rejection: %q", progress.String())
	}

	if err := Rollback(); !errors.Is(err, ErrSelfUpdateDisabled) {
		t.Fatalf("Rollback() error = %v, want %v", err, ErrSelfUpdateDisabled)
	}
}
