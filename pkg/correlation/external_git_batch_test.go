package correlation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadExternalCommitsBatchMatchesSingleCommitSemantics(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	repositories := []struct {
		name string
		key  string
	}{
		{name: "repository-a", key: "ctx:repository-a"},
		{name: "repository-b", key: "ctx:repository-b"},
	}
	root := t.TempDir()
	allCommits := make(map[string][]string, len(repositories))
	for _, repository := range repositories {
		repoPath := filepath.Join(root, repository.name)
		if err := os.MkdirAll(repoPath, 0o700); err != nil {
			t.Fatal(err)
		}
		runTestGit(t, gitPath, repoPath, "init", "--quiet")
		runTestGit(t, gitPath, repoPath, "config", "user.name", "Batch Test")
		runTestGit(t, gitPath, repoPath, "config", "user.email", "batch@example.invalid")
		for i := 0; i < 12; i++ {
			path := filepath.Join(repoPath, fmt.Sprintf("src-%02d.go", i))
			if err := os.WriteFile(path, []byte(fmt.Sprintf("package p\nvar Value%d = %d\n", i, i)), 0o600); err != nil {
				t.Fatal(err)
			}
			runTestGit(t, gitPath, repoPath, "add", ".")
			runTestGitWithDate(t, gitPath, repoPath, []string{"commit", "--quiet", "-m", fmt.Sprintf("commit %02d", i)}, i)
			allCommits[repository.key] = append(allCommits[repository.key], strings.TrimSpace(runTestGit(t, gitPath, repoPath, "rev-parse", "HEAD")))
		}
		oldPath := filepath.Join(repoPath, "src-00.go")
		newPath := filepath.Join(repoPath, "renamed.go")
		if err := os.Rename(oldPath, newPath); err != nil {
			t.Fatal(err)
		}
		runTestGit(t, gitPath, repoPath, "add", "-A")
		runTestGitWithDate(t, gitPath, repoPath, []string{"commit", "--quiet", "-m", "rename source"}, 20)
		allCommits[repository.key] = append(allCommits[repository.key], strings.TrimSpace(runTestGit(t, gitPath, repoPath, "rev-parse", "HEAD")))
	}

	logPath := filepath.Join(root, "git-invocations.log")
	binPath := filepath.Join(root, "bin")
	if err := os.Mkdir(binPath, 0o700); err != nil {
		t.Fatal(err)
	}
	wrapper := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\nexec %q \"$@\"\n", logPath, gitPath)
	if err := os.WriteFile(filepath.Join(binPath, "git"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	wantByRepository := make(map[string]map[string]CorrelatedCommit, len(repositories))
	legacyStart := time.Now()
	for _, repository := range repositories {
		repoPath := filepath.Join(root, repository.name)
		wantByRepository[repository.key] = make(map[string]CorrelatedCommit, len(allCommits[repository.key]))
		for _, sha := range allCommits[repository.key] {
			wantByRepository[repository.key][strings.ToLower(sha)] = singleExternalCommitForTest(t, gitPath, repository.key, repoPath, sha)
		}
	}
	legacyElapsed := time.Since(legacyStart)
	t.Setenv("PATH", binPath+string(os.PathListSeparator)+os.Getenv("PATH"))

	correlator := &Correlator{ctx: context.Background()}
	batchStart := time.Now()
	for _, repository := range repositories {
		repoPath := filepath.Join(root, repository.name)
		requested := allCommits[repository.key]
		got, err := correlator.loadExternalCommits(repository.key, repoPath, requested)
		if err != nil {
			t.Fatalf("loadExternalCommits(%s): %v", repository.key, err)
		}
		for _, sha := range requested {
			want := wantByRepository[repository.key][strings.ToLower(sha)]
			if !reflect.DeepEqual(got[strings.ToLower(sha)].commit, want) {
				t.Fatalf("batch commit %s differs from single-commit semantics:\ngot=%#v\nwant=%#v", sha, got[strings.ToLower(sha)].commit, want)
			}
		}
	}
	batchElapsed := time.Since(batchStart)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	invocations := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(invocations) != len(repositories)*4 {
		t.Fatalf("Git subprocesses = %d, want %d (four batched processes per repository):\n%s", len(invocations), len(repositories)*4, data)
	}
	wantLegacy := 0
	for _, commits := range allCommits {
		wantLegacy += len(commits) * 4
	}
	t.Logf("Git subprocess boundary: legacy=%d, batched=%d", wantLegacy, len(invocations))
	t.Logf("Git hydration fixture elapsed: legacy=%s, batched=%s", legacyElapsed, batchElapsed)
}

func TestLoadExternalCommitsBatchReportsMissingCommit(t *testing.T) {
	repoPath := disposableGitRepository(t)
	sha := strings.TrimSpace(runTestGit(t, "git", repoPath, "rev-parse", "HEAD"))
	correlator := &Correlator{ctx: context.Background()}
	missing := strings.Repeat("0", 40)
	results, err := correlator.loadExternalCommits("ctx:test", repoPath, []string{sha, missing})
	if err != nil {
		t.Fatal(err)
	}
	if results[missing].err == nil || !strings.Contains(results[missing].err.Error(), "is absent from repository") || !strings.Contains(results[missing].err.Error(), missing) {
		t.Fatalf("missing commit error = %v", results[missing].err)
	}
}

func TestBatchExternalMetadataRejectsMalformedOutput(t *testing.T) {
	correlator := &Correlator{ctx: context.Background()}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nprintf 'not metadata\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err := correlator.batchExternalCommitMetadata(t.TempDir(), []string{strings.Repeat("a", 40)})
	if err == nil || !strings.Contains(err.Error(), "unexpected Git output") {
		t.Fatalf("malformed metadata error = %v", err)
	}
}

func TestLoadExternalCommitsBatchHonorsCancellation(t *testing.T) {
	repoPath := disposableGitRepository(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	correlator := &Correlator{ctx: ctx}
	_, err := correlator.loadExternalCommits("ctx:test", repoPath, []string{strings.TrimSpace(runTestGit(t, "git", repoPath, "rev-parse", "HEAD"))})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled batch error = %v, want context canceled", err)
	}
}

func singleExternalCommitForTest(t *testing.T, gitPath, repository, repoPath, sha string) CorrelatedCommit {
	t.Helper()
	resolved := strings.TrimSpace(runTestGit(t, gitPath, repoPath, "rev-parse", "--verify", sha+"^{commit}"))
	metadata := runTestGit(t, gitPath, repoPath, "show", "-s", "--format=%H%x00%an%x00%ae%x00%aI%x00%B", resolved)
	parts := bytes.SplitN([]byte(metadata), []byte{0}, 5)
	if len(parts) != 5 {
		t.Fatalf("single metadata for %s = %q", sha, metadata)
	}
	timestamp, err := time.Parse(time.RFC3339, strings.TrimSpace(string(parts[3])))
	if err != nil {
		t.Fatal(err)
	}
	files := parseNameStatus([]byte(runTestGit(t, gitPath, repoPath, "show", "--find-renames", "--name-status", "--format=", resolved)))
	stats := parseNumstat([]byte(runTestGit(t, gitPath, repoPath, "show", "--find-renames", "--numstat", "--format=", resolved)))
	filtered := make([]FileChange, 0, len(files))
	for _, file := range files {
		if isExcludedPath(file.Path) {
			continue
		}
		file.Repository = repository
		if stat, ok := stats[file.Path]; ok {
			file.Insertions = stat.insertions
			file.Deletions = stat.deletions
		}
		filtered = append(filtered, file)
	}
	return CorrelatedCommit{
		Repository: repository,
		SHA:        strings.TrimSpace(string(parts[0])), ShortSHA: shortSHA(resolved),
		Message: strings.TrimSpace(string(parts[4])), Author: string(parts[1]), AuthorEmail: string(parts[2]),
		Timestamp: timestamp, Files: filtered, Method: MethodExternalLedger, Confidence: 1,
		Reason: "Explicit Beads Hub correlation",
	}
}

func disposableGitRepository(t *testing.T) string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	repoPath := filepath.Join(t.TempDir(), "repository")
	if err := os.Mkdir(repoPath, 0o700); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, gitPath, repoPath, "init", "--quiet")
	runTestGit(t, gitPath, repoPath, "config", "user.name", "Batch Test")
	runTestGit(t, gitPath, repoPath, "config", "user.email", "batch@example.invalid")
	if err := os.WriteFile(filepath.Join(repoPath, "file.go"), []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, gitPath, repoPath, "add", ".")
	runTestGit(t, gitPath, repoPath, "commit", "--quiet", "-m", "initial")
	return repoPath
}

func runTestGit(t *testing.T, gitPath, repoPath string, args ...string) string {
	t.Helper()
	cmd := exec.Command(gitPath, args...)
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func runTestGitWithDate(t *testing.T, gitPath, repoPath string, args []string, minute int) {
	t.Helper()
	cmd := exec.Command(gitPath, args...)
	cmd.Dir = repoPath
	date := fmt.Sprintf("2026-01-01T00:%02d:00Z", minute)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
