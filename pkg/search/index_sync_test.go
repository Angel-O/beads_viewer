package search

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSemanticIndexPathHubStore(t *testing.T) {
	root := t.TempDir()
	hubStore := filepath.Join(root, "hub", ".beads")
	got, err := SemanticIndexPath(filepath.Join(root, "source", ".beads", "issues.jsonl"), hubStore, EmbeddingConfig{Provider: ProviderHash, Dim: 384})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "hub", "semantic", "index-hash-384.bvvi")
	if got != want {
		t.Fatalf("SemanticIndexPath() = %q, want %q", got, want)
	}
}

func TestSemanticIndexPathLocalRepositoryIdentity(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("BV_CACHE_DIR", cache)
	root := t.TempDir()
	primary := filepath.Join(root, "primary")
	linked := filepath.Join(root, "linked")
	initSemanticTestRepository(t, primary)
	runSemanticTestGit(t, primary, "worktree", "add", "-b", "linked", linked)

	primaryDataset := writeSemanticTestDataset(t, primary)
	linkedDataset := writeSemanticTestDataset(t, linked)
	primaryPath, err := SemanticIndexPath(primaryDataset, "", EmbeddingConfig{Provider: ProviderHash, Dim: 384})
	if err != nil {
		t.Fatal(err)
	}
	linkedPath, err := SemanticIndexPath(linkedDataset, "", EmbeddingConfig{Provider: ProviderHash, Dim: 384})
	if err != nil {
		t.Fatal(err)
	}
	if primaryPath != linkedPath {
		t.Fatalf("linked worktrees used different cache paths:\nprimary: %s\nlinked:  %s", primaryPath, linkedPath)
	}
	if !strings.HasPrefix(primaryPath, filepath.Join(cache, "semantic")+string(filepath.Separator)) {
		t.Fatalf("local index path %q is outside cache %q", primaryPath, cache)
	}
	if strings.HasPrefix(primaryPath, primary+string(filepath.Separator)) {
		t.Fatalf("local index path %q is inside repository %q", primaryPath, primary)
	}
}

func TestSemanticIndexPathSeparatesRepositoriesProvidersAndDimensions(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("BV_CACHE_DIR", cache)
	root := t.TempDir()
	repoA := filepath.Join(root, "a", "same-name")
	repoB := filepath.Join(root, "b", "same-name")
	initSemanticTestRepository(t, repoA)
	initSemanticTestRepository(t, repoB)
	datasetA := writeSemanticTestDataset(t, repoA)
	datasetB := writeSemanticTestDataset(t, repoB)

	hash384, err := SemanticIndexPath(datasetA, "", EmbeddingConfig{Provider: ProviderHash, Dim: 384})
	if err != nil {
		t.Fatal(err)
	}
	hash768, err := SemanticIndexPath(datasetA, "", EmbeddingConfig{Provider: ProviderHash, Dim: 768})
	if err != nil {
		t.Fatal(err)
	}
	openAI384, err := SemanticIndexPath(datasetA, "", EmbeddingConfig{Provider: ProviderOpenAI, Dim: 384})
	if err != nil {
		t.Fatal(err)
	}
	otherRepo, err := SemanticIndexPath(datasetB, "", EmbeddingConfig{Provider: ProviderHash, Dim: 384})
	if err != nil {
		t.Fatal(err)
	}
	workspaceDataset := filepath.Join(repoA, ".bv", "other-workspace.yaml")
	if err := os.MkdirAll(filepath.Dir(workspaceDataset), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workspaceDataset, []byte("repositories: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	otherDataset, err := SemanticIndexPath(workspaceDataset, "", EmbeddingConfig{Provider: ProviderHash, Dim: 384})
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{
		"hash-384":   hash384,
		"hash-768":   hash768,
		"openai-384": openAI384,
		"other-repo": otherRepo,
		"other-data": otherDataset,
	}
	seen := make(map[string]string, len(paths))
	for name, path := range paths {
		if previous, exists := seen[path]; exists {
			t.Fatalf("%s and %s share semantic index path %q", name, previous, path)
		}
		seen[path] = name
	}
	if filepath.Dir(hash384) == filepath.Dir(otherRepo) {
		t.Fatalf("same-basename repositories share cache key: %q", filepath.Dir(hash384))
	}
}

func initSemanticTestRepository(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	runSemanticTestGit(t, root, "init", "-b", "main")
	runSemanticTestGit(t, root, "config", "user.email", "test@example.com")
	runSemanticTestGit(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runSemanticTestGit(t, root, "add", "README.md")
	runSemanticTestGit(t, root, "commit", "-m", "initial")
}

func writeSemanticTestDataset(t *testing.T, root string) string {
	t.Helper()
	directory := filepath.Join(root, ".beads")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "issues.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runSemanticTestGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}

type failOnCallEmbedder struct {
	dim      int
	failCall int
	calls    int
}

type mutatingIndexEmbedder struct {
	idx *VectorIndex
}

func (*mutatingIndexEmbedder) Provider() Provider { return ProviderHash }
func (*mutatingIndexEmbedder) Dim() int           { return 2 }
func (e *mutatingIndexEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if err := e.idx.Upsert("concurrent", ComputeContentHash("concurrent"), []float32{0, 1}); err != nil {
		return nil, err
	}
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = []float32{1, 0}
	}
	return vectors, nil
}

func (*failOnCallEmbedder) Provider() Provider { return ProviderHash }
func (e *failOnCallEmbedder) Dim() int         { return e.dim }
func (e *failOnCallEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls++
	if e.calls == e.failCall {
		return nil, errors.New("injected embedding failure")
	}
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = []float32{0, 1}
	}
	return vectors, nil
}

func TestSyncVectorIndex_IncrementalUpdates(t *testing.T) {
	embedder, err := NewEmbedderFromConfig(EmbeddingConfig{Provider: ProviderHash, Dim: 16})
	if err != nil {
		t.Fatalf("NewEmbedderFromConfig: %v", err)
	}

	idx := NewVectorIndex(embedder.Dim())
	docs1 := map[string]string{
		"A": "Fix login flow\nAdd OAuth redirect handling",
		"B": "Update docs\nReadme improvements",
	}

	stats, err := SyncVectorIndex(context.Background(), idx, embedder, docs1, 0)
	if err != nil {
		t.Fatalf("SyncVectorIndex: %v", err)
	}
	if stats.Added != 2 || stats.Embedded != 2 || stats.Updated != 0 || stats.Removed != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if idx.Size() != 2 {
		t.Fatalf("expected 2 entries, got %d", idx.Size())
	}

	// Second sync with identical docs should not re-embed.
	stats2, err := SyncVectorIndex(context.Background(), idx, embedder, docs1, 0)
	if err != nil {
		t.Fatalf("SyncVectorIndex: %v", err)
	}
	if stats2.Skipped != 2 || stats2.Embedded != 0 || stats2.Added != 0 || stats2.Updated != 0 || stats2.Removed != 0 {
		t.Fatalf("unexpected stats: %+v", stats2)
	}

	// Change A, remove B, add C.
	docs2 := map[string]string{
		"A": "Fix login flow\nHandle PKCE code verifier",
		"C": "Add tests\nCover edge cases",
	}
	stats3, err := SyncVectorIndex(context.Background(), idx, embedder, docs2, 0)
	if err != nil {
		t.Fatalf("SyncVectorIndex: %v", err)
	}
	if stats3.Updated != 1 || stats3.Added != 1 || stats3.Removed != 1 || stats3.Embedded != 2 {
		t.Fatalf("unexpected stats: %+v", stats3)
	}
	if idx.Size() != 2 {
		t.Fatalf("expected 2 entries after update, got %d", idx.Size())
	}
	if _, ok := idx.Get("B"); ok {
		t.Fatalf("expected B to be removed")
	}
	if _, ok := idx.Get("C"); !ok {
		t.Fatalf("expected C to be present")
	}
}

func TestSyncVectorIndexFailureLeavesIndexUnchanged(t *testing.T) {
	idx := NewVectorIndex(2)
	originalHash := ComputeContentHash("old A")
	if err := idx.Upsert("A", originalHash, []float32{1, 0}); err != nil {
		t.Fatalf("Upsert A: %v", err)
	}
	if err := idx.Upsert("stale", ComputeContentHash("stale"), []float32{1, 0}); err != nil {
		t.Fatalf("Upsert stale: %v", err)
	}

	embedder := &failOnCallEmbedder{dim: 2, failCall: 2}
	stats, err := SyncVectorIndex(context.Background(), idx, embedder, map[string]string{
		"A": "new A",
		"C": "new C",
	}, 1)
	if err == nil {
		t.Fatal("expected injected embedding failure")
	}
	if stats != (IndexSyncStats{Total: 2}) {
		t.Fatalf("failed sync reported unapplied changes: %+v", stats)
	}
	if idx.Size() != 2 {
		t.Fatalf("failed sync changed index size to %d", idx.Size())
	}
	a, ok := idx.Get("A")
	if !ok || a.ContentHash != originalHash || len(a.Vector) != 2 || a.Vector[0] != 1 || a.Vector[1] != 0 {
		t.Fatalf("failed sync changed A: %#v, present=%t", a, ok)
	}
	if _, ok := idx.Get("stale"); !ok {
		t.Fatal("failed sync removed stale entry before commit")
	}
	if _, ok := idx.Get("C"); ok {
		t.Fatal("failed sync published a staged entry")
	}
}

func TestSyncVectorIndexDoesNotClobberConcurrentMutation(t *testing.T) {
	idx := NewVectorIndex(2)
	originalHash := ComputeContentHash("old A")
	if err := idx.Upsert("A", originalHash, []float32{0, 1}); err != nil {
		t.Fatalf("Upsert A: %v", err)
	}

	stats, err := SyncVectorIndex(context.Background(), idx, &mutatingIndexEmbedder{idx: idx}, map[string]string{
		"A": "new A",
	}, 1)
	if err == nil || !strings.Contains(err.Error(), "changed concurrently") {
		t.Fatalf("SyncVectorIndex() error = %v, want concurrent-change refusal", err)
	}
	if stats != (IndexSyncStats{Total: 1}) {
		t.Fatalf("failed sync reported unapplied changes: %+v", stats)
	}
	a, ok := idx.Get("A")
	if !ok || a.ContentHash != originalHash {
		t.Fatalf("failed sync changed original A: %#v, present=%t", a, ok)
	}
	if _, ok := idx.Get("concurrent"); !ok {
		t.Fatal("failed sync clobbered the concurrent writer's entry")
	}
}

func TestLoadOrNewVectorIndex(t *testing.T) {
	embedder := NewHashEmbedder(8)
	path := filepath.Join(t.TempDir(), "semantic", "index.bvvi")

	idx, loaded, err := LoadOrNewVectorIndex(path, embedder.Dim())
	if err != nil {
		t.Fatalf("LoadOrNewVectorIndex: %v", err)
	}
	if loaded {
		t.Fatalf("expected loaded=false for missing file")
	}
	if idx.Dim != embedder.Dim() {
		t.Fatalf("dim mismatch: got %d want %d", idx.Dim, embedder.Dim())
	}

	if err := idx.Upsert("A", ComputeContentHash("a"), []float32{1, 0, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := idx.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loadedIdx, loaded2, err := LoadOrNewVectorIndex(path, embedder.Dim())
	if err != nil {
		t.Fatalf("LoadOrNewVectorIndex: %v", err)
	}
	if !loaded2 {
		t.Fatalf("expected loaded=true after save")
	}
	if loadedIdx.Size() != 1 {
		t.Fatalf("expected 1 entry, got %d", loadedIdx.Size())
	}
}

func TestLoadOrNewVectorIndexRebuildsWrongDimension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantic", "index.bvvi")
	original := NewVectorIndex(2)
	if err := original.Upsert("A", ComputeContentHash("a"), []float32{1, 0}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := original.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	idx, loaded, err := LoadOrNewVectorIndex(path, 3)
	if err != nil {
		t.Fatalf("LoadOrNewVectorIndex: %v", err)
	}
	if loaded {
		t.Fatalf("wrong-dimension index reported as loaded")
	}
	if idx.Dim != 3 || idx.Size() != 0 {
		t.Fatalf("rebuilt index = dim %d size %d, want dim 3 size 0", idx.Dim, idx.Size())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("wrong-dimension index remained at live path: %v", err)
	}
	backups, err := filepath.Glob(path + ".corrupt-*")
	if err != nil {
		t.Fatalf("glob backups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("backup count = %d, want 1: %v", len(backups), backups)
	}
}

func TestLoadOrNewVectorIndexPreservesRapidCorruptBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantic", "index.bvvi")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	wantContents := map[string]bool{
		"first corrupt index":  false,
		"second corrupt index": false,
	}
	for contents := range wantContents {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q): %v", contents, err)
		}
		idx, loaded, err := LoadOrNewVectorIndex(path, 8)
		if err != nil {
			t.Fatalf("LoadOrNewVectorIndex(%q): %v", contents, err)
		}
		if loaded || idx.Dim != 8 || idx.Size() != 0 {
			t.Fatalf("recovery(%q) = loaded %t, dim %d, size %d", contents, loaded, idx.Dim, idx.Size())
		}
	}

	backups, err := filepath.Glob(path + ".corrupt-*")
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(backups) != len(wantContents) {
		t.Fatalf("backup count = %d, want %d: %v", len(backups), len(wantContents), backups)
	}
	for _, backup := range backups {
		data, err := os.ReadFile(backup)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", backup, err)
		}
		contents := string(data)
		if _, ok := wantContents[contents]; !ok {
			t.Fatalf("unexpected backup contents %q in %s", contents, backup)
		}
		wantContents[contents] = true
	}
	for contents, preserved := range wantContents {
		if !preserved {
			t.Fatalf("corrupt artifact %q was overwritten", contents)
		}
	}
}
