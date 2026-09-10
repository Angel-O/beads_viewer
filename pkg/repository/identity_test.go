package repository

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestRepositoryIdentityResolvesModuleWorktree(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root, common, err := RepositoryIdentity(filepath.Join(filepath.Dir(source), "../.."))
	if err != nil {
		t.Fatal(err)
	}
	if root == "" || common == "" || !filepath.IsAbs(root) || !filepath.IsAbs(common) {
		t.Fatalf("RepositoryIdentity() = root %q, common %q; want absolute paths", root, common)
	}
}
