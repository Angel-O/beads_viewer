package ui

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
)

func TestGitRemoteToWebURL(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		want   string
	}{
		{
			name:   "scp style github",
			remote: "git@github.com:owner/repo.git",
			want:   "https://github.com/owner/repo",
		},
		{
			name:   "ssh URL github",
			remote: "ssh://git@github.com/owner/repo.git",
			want:   "https://github.com/owner/repo",
		},
		{
			name:   "ssh URL gitlab nested group",
			remote: "ssh://git@gitlab.com/group/subgroup/repo.git",
			want:   "https://gitlab.com/group/subgroup/repo",
		},
		{
			name:   "ssh URL drops ssh port",
			remote: "ssh://git@github.com:2222/owner/repo.git",
			want:   "https://github.com/owner/repo",
		},
		{
			name:   "https trims suffix and query",
			remote: "https://github.com/owner/repo.git?ignored=1",
			want:   "https://github.com/owner/repo",
		},
		{
			name:   "empty path rejected",
			remote: "ssh://git@github.com",
			want:   "",
		},
		{
			name:   "unsupported scheme rejected",
			remote: "file:///tmp/repo.git",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gitRemoteToWebURL(tt.remote)
			if strings.Compare(got, tt.want) != 0 {
				t.Fatalf("gitRemoteToWebURL(%q) = %q, want %q", tt.remote, got, tt.want)
			}
		})
	}
}

func TestGetCommitURLUsesCorrelatedCommitRepository(t *testing.T) {
	localRepository := testGitRepository(t, "git@github.com:owner/local.git")
	githubRepository := testGitRepository(t, "https://github.com/owner/github.git")
	gitlabRepository := testGitRepository(t, "ssh://git@gitlab.com/group/gitlab.git")
	m := Model{workDir: localRepository, repositoryScopeController: repositoryScopeController{repositoryCatalog: repositorypkg.Catalog{
		{ID: "ctx:github-111", Name: "github", Path: githubRepository, Kind: repositorypkg.IdentityExact},
		{ID: "ctx:gitlab-222", Name: "gitlab", Path: gitlabRepository, Kind: repositorypkg.IdentityExact},
	}}}

	tests := []struct {
		name       string
		repository string
		want       string
	}{
		{name: "same repository", want: "https://github.com/owner/local/commit/abc123"},
		{name: "registered GitHub repository", repository: "ctx:github-111", want: "https://github.com/owner/github/commit/abc123"},
		{name: "registered GitLab repository", repository: "ctx:gitlab-222", want: "https://gitlab.com/group/gitlab/commit/abc123"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := m.getCommitURL(test.repository, "abc123")
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("getCommitURL(%q) = %q, want %q", test.repository, got, test.want)
			}
		})
	}
	if _, err := m.getCommitURL("ctx:missing-333", "abc123"); err == nil || !strings.Contains(err.Error(), "repository catalog") {
		t.Fatalf("missing catalog repository error = %v", err)
	}
}

func TestHistoryOpenCommit(t *testing.T) {
	repository := testGitRepository(t, "git@github.com:owner/external.git")
	m := historyOpenTestModel("ctx:external-111", "abcdef123456", repositorypkg.Catalog{
		{ID: "ctx:external-111", Name: "external", Path: repository, Kind: repositorypkg.IdentityExact},
	})
	var openedURL string
	m.browserOpener = func(url string) error {
		openedURL = url
		return nil
	}

	updated, _ := m.Update(keyMsg("o"))
	m = updated.(*Model)
	if openedURL != "https://github.com/owner/external/commit/abcdef123456" {
		t.Fatalf("opened URL = %q", openedURL)
	}
	if m.statusIsError || m.statusMsg != "🌐 Opened abcdef1 in browser" {
		t.Fatalf("status = %q, error=%v", m.statusMsg, m.statusIsError)
	}
}

func TestHistoryOpenCommitReportsOpenerFailureInGitMode(t *testing.T) {
	repository := testGitRepository(t, "https://github.com/owner/external.git")
	m := historyOpenTestModel("ctx:external-111", "abcdef123456", repositorypkg.Catalog{
		{ID: "ctx:external-111", Name: "external", Path: repository, Kind: repositorypkg.IdentityExact},
	})
	m.historyView.ToggleViewMode()
	m.browserOpener = func(string) error { return errors.New("opener unavailable") }

	updated, _ := m.Update(keyMsg("o"))
	m = updated.(*Model)
	if !m.statusIsError || !strings.Contains(m.statusMsg, "opener unavailable") {
		t.Fatalf("status = %q, error=%v", m.statusMsg, m.statusIsError)
	}
}

func TestHistoryOpenCommitReportsMissingMetadata(t *testing.T) {
	noRemoteRepository := testGitRepository(t, "")

	tests := []struct {
		name       string
		repository string
		sha        string
		want       string
	}{
		{name: "commit", want: "No commit selected"},
		{name: "repository", sha: "abc123", want: "commit repository is unavailable"},
		{name: "catalog registration", repository: "ctx:missing-222", sha: "abc123", want: "is not available in the repository catalog"},
		{name: "remote", repository: "ctx:no-remote-111", sha: "abc123", want: "reading origin remote"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := historyOpenTestModel(test.repository, test.sha, repositorypkg.Catalog{
				{ID: "ctx:no-remote-111", Name: "no-remote", Path: noRemoteRepository, Kind: repositorypkg.IdentityExact},
			})
			m.browserOpener = func(string) error {
				t.Fatal("browser opener called with incomplete metadata")
				return nil
			}

			updated, _ := m.Update(keyMsg("o"))
			m = updated.(*Model)
			if !m.statusIsError || !strings.Contains(m.statusMsg, test.want) {
				t.Fatalf("status = %q, error=%v; want %q", m.statusMsg, m.statusIsError, test.want)
			}
		})
	}
}

func historyOpenTestModel(repository, sha string, catalog repositorypkg.Catalog) *Model {
	report := &correlation.HistoryReport{Histories: map[string]correlation.BeadHistory{}}
	if sha != "" {
		report.Histories["global-test"] = correlation.BeadHistory{
			BeadID: "global-test",
			Commits: []correlation.CorrelatedCommit{{
				Repository: repository,
				SHA:        sha,
				ShortSHA:   sha,
				Timestamp:  time.Now(),
			}},
		}
	}
	m := NewModel(nil, nil, "")
	m.repositoryCatalog = catalog
	m.hubRepositoryMode = true
	m.historyView = NewHistoryModel(report, testTheme())
	makeHistoryReportCurrent(m, report)
	m.isHistoryView = true
	m.focused = focusHistory
	return m
}

func testGitRepository(t *testing.T, remote string) string {
	t.Helper()
	repository := t.TempDir()
	runGitForHistoryOpenTest(t, repository, "init")
	if remote != "" {
		runGitForHistoryOpenTest(t, repository, "remote", "add", "origin", remote)
	}
	return repository
}

func runGitForHistoryOpenTest(t *testing.T, repository string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repository
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}
