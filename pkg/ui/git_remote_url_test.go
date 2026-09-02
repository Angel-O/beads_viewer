package ui

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
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
	configPath := testHubConfig(t, map[string]string{
		"ctx:github-111": githubRepository,
		"ctx:gitlab-222": gitlabRepository,
	})
	m := Model{workDir: localRepository, runtimeServices: RuntimeServices{CatalogPath: configPath}}

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
}

func TestHistoryOpenCommit(t *testing.T) {
	repository := testGitRepository(t, "git@github.com:owner/external.git")
	configPath := testHubConfig(t, map[string]string{"ctx:external-111": repository})
	m := historyOpenTestModel("ctx:external-111", "abcdef123456", configPath)
	var openedURL string
	m.browserOpener = func(url string) error {
		openedURL = url
		return nil
	}

	updated, _ := m.Update(keyMsg("o"))
	m = updated.(Model)
	if openedURL != "https://github.com/owner/external/commit/abcdef123456" {
		t.Fatalf("opened URL = %q", openedURL)
	}
	if m.statusIsError || m.statusMsg != "🌐 Opened abcdef1 in browser" {
		t.Fatalf("status = %q, error=%v", m.statusMsg, m.statusIsError)
	}
}

func TestHistoryOpenCommitReportsOpenerFailureInGitMode(t *testing.T) {
	repository := testGitRepository(t, "https://github.com/owner/external.git")
	configPath := testHubConfig(t, map[string]string{"ctx:external-111": repository})
	m := historyOpenTestModel("ctx:external-111", "abcdef123456", configPath)
	m.historyView.ToggleViewMode()
	m.browserOpener = func(string) error { return errors.New("opener unavailable") }

	updated, _ := m.Update(keyMsg("o"))
	m = updated.(Model)
	if !m.statusIsError || !strings.Contains(m.statusMsg, "opener unavailable") {
		t.Fatalf("status = %q, error=%v", m.statusMsg, m.statusIsError)
	}
}

func TestHistoryOpenCommitReportsMissingMetadata(t *testing.T) {
	noRemoteRepository := testGitRepository(t, "")
	configPath := testHubConfig(t, map[string]string{"ctx:no-remote-111": noRemoteRepository})

	tests := []struct {
		name       string
		repository string
		sha        string
		want       string
	}{
		{name: "commit", want: "No commit selected"},
		{name: "repository", sha: "abc123", want: "commit repository is unavailable"},
		{name: "Hub registration", repository: "ctx:missing-222", sha: "abc123", want: "is not registered in the Hub"},
		{name: "remote", repository: "ctx:no-remote-111", sha: "abc123", want: "reading origin remote"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := historyOpenTestModel(test.repository, test.sha, configPath)
			m.browserOpener = func(string) error {
				t.Fatal("browser opener called with incomplete metadata")
				return nil
			}

			updated, _ := m.Update(keyMsg("o"))
			m = updated.(Model)
			if !m.statusIsError || !strings.Contains(m.statusMsg, test.want) {
				t.Fatalf("status = %q, error=%v; want %q", m.statusMsg, m.statusIsError, test.want)
			}
		})
	}
}

func historyOpenTestModel(repository, sha, configPath string) Model {
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
	m.runtimeServices.CatalogPath = configPath
	m.historyView = NewHistoryModel(report, testTheme())
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

func testHubConfig(t *testing.T, repositories map[string]string) string {
	t.Helper()
	config := hub.Config{
		Version:      hub.ConfigVersion,
		Store:        "store",
		Ledger:       "ledger",
		Repositories: make(map[string]hub.Repository, len(repositories)),
	}
	for context, repository := range repositories {
		config.Repositories[context] = hub.Repository{Path: repository}
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "hub.json")
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath
}
