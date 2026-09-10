package hub

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const hubImportPath = "github.com/Dicklesworthstone/beads_viewer/pkg/hub"

// temporaryHubImportExceptions records only current neutral-package seams.
// Each entry is intentionally file-specific so the owning migration phase can
// remove it without weakening the final package boundary.
var temporaryHubImportExceptions = map[string]string{
	"pkg/correlation/hub_config.go": "correlation configuration seam",
	"pkg/search/index_sync.go":      "semantic-index synchronization seam",
}

var hubImportAllowedRoots = []string{
	"pkg/hub/",
	"cmd/bv/",
	"cmd/wbd/",
	"cmd/wbv/",
}

var explicitHubE2ETests = map[string]bool{
	"tests/e2e/hub_context_scope_e2e_test.go": true,
}

func TestHubImportBoundary(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	violations, usedExceptions, err := scanHubImports(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("pkg/hub imports outside the migration boundary: %s", strings.Join(violations, ", "))
	}
	exceptions := make([]string, 0, len(temporaryHubImportExceptions))
	for path := range temporaryHubImportExceptions {
		exceptions = append(exceptions, path)
	}
	sort.Strings(exceptions)
	for _, path := range exceptions {
		reason := temporaryHubImportExceptions[path]
		if !usedExceptions[path] {
			t.Errorf("temporary Hub import exception %s is unused (%s)", path, reason)
		}
	}
}

func TestHubImportBoundaryPolicy(t *testing.T) {
	for _, path := range []string{"pkg/hub/hub.go", "cmd/bv/main.go", "cmd/wbd/app.go", "cmd/wbv/main.go", "tests/e2e/hub_context_scope_e2e_test.go"} {
		if !hubImportIsAllowed(path) {
			t.Errorf("Hub import path %s was rejected", path)
		}
	}
	for _, path := range []string{"pkg/ui/model.go", "pkg/search/index_sync.go", "pkg/loader/loader.go"} {
		if hubImportIsAllowed(path) {
			t.Errorf("Hub import path %s was allowed", path)
		}
	}
}

func scanHubImports(root string) ([]string, map[string]bool, error) {
	violations := make(map[string]bool)
	usedExceptions := make(map[string]bool)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil || importPath != hubImportPath {
				continue
			}
			if hubImportIsAllowed(relative) {
				continue
			}
			if _, ok := temporaryHubImportExceptions[relative]; ok {
				usedExceptions[relative] = true
				continue
			}
			violations[relative] = true
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	result := make([]string, 0, len(violations))
	for path := range violations {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, usedExceptions, nil
}

func hubImportIsAllowed(path string) bool {
	for _, root := range hubImportAllowedRoots {
		if strings.HasPrefix(path, root) {
			return true
		}
	}
	return explicitHubE2ETests[path]
}
