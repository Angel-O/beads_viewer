package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func rewriteGitHubDownloadsToServer(t *testing.T, client *http.Client, serverURL string) {
	t.Helper()
	target, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		clonedURL := *req.URL
		clone.URL = &clonedURL
		if req.URL.Hostname() == "github.com" && strings.Contains(req.URL.Path, "/releases/download/") {
			clone.URL.Scheme = target.Scheme
			clone.URL.Host = target.Host
			if strings.HasSuffix(req.URL.Path, "/checksums.txt") {
				clone.URL.Path = "/checksums"
			}
		}
		return base.RoundTrip(clone)
	})
}

// TestIsGitHubHost verifies the domain allow-list for token transmission.
func TestIsGitHubHost(t *testing.T) {
	tests := []struct {
		rawURL string
		want   bool
	}{
		{"https://api.github.com/repos/foo/bar", true},
		{"https://github.com/releases/download/v1.0/file.tar.gz", true},
		{"https://objects.githubusercontent.com/something", true},
		{"https://raw.githubusercontent.com/foo/bar/main/file", true},
		{"https://githubusercontent.com/something", true},
		{"https://evil.com", false},
		{"https://notgithub.com/repos", false},
		{"https://github.com.evil.com/phish", false},
		{"http://localhost:8080/test", false},
		{"https://example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.rawURL, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tt.rawURL, nil)
			if err != nil {
				t.Fatalf("bad test URL %q: %v", tt.rawURL, err)
			}
			if got := isGitHubHost(req.URL); got != tt.want {
				t.Errorf("isGitHubHost(%q) = %v, want %v", tt.rawURL, got, tt.want)
			}
		})
	}
}

func TestCheckForUpdates_Network(t *testing.T) {
	// We cannot replace version.Version without link-time substitution, so the
	// fixtures use an obviously newer tag and an obviously older tag.

	validChecksumBody := strings.TrimPrefix(testSHA256DigestA, "sha256:") + "  " + stableAssetName() + "\n"
	installableReleaseBody := func(tag, checksumBody string) string {
		checksumHash := sha256.Sum256([]byte(checksumBody))
		release := Release{
			TagName: tag,
			HTMLURL: "https://github.com/Dicklesworthstone/beads_viewer/releases/tag/" + tag,
			Assets: []Asset{
				{
					Name:               stableAssetName(),
					BrowserDownloadURL: "https://github.com/Dicklesworthstone/beads_viewer/releases/download/" + tag + "/" + stableAssetName(),
					Size:               1024,
					Digest:             testSHA256DigestA,
					State:              "uploaded",
				},
				{
					Name:               "checksums.txt",
					BrowserDownloadURL: "https://github.com/Dicklesworthstone/beads_viewer/releases/download/" + tag + "/checksums.txt",
					Size:               int64(len(checksumBody)),
					Digest:             "sha256:" + hex.EncodeToString(checksumHash[:]),
					State:              "uploaded",
				},
			},
		}
		data, err := json.Marshal(release)
		if err != nil {
			t.Fatalf("marshal release fixture: %v", err)
		}
		return string(data)
	}

	tests := []struct {
		name           string
		responseBody   string
		responseStatus int
		expectTag      string
		expectURL      string
		expectErr      bool
		checksumBody   string
	}{
		{
			name:           "Newer version available",
			responseBody:   installableReleaseBody("v99.0.0", validChecksumBody),
			responseStatus: http.StatusOK,
			expectTag:      "v99.0.0",
			expectURL:      "https://github.com/Dicklesworthstone/beads_viewer/releases/tag/v99.0.0",
			expectErr:      false,
			checksumBody:   validChecksumBody,
		},
		{
			name:           "Newer release manifest disagrees with asset digest",
			responseBody:   installableReleaseBody("v99.0.0", strings.TrimPrefix(testSHA256DigestB, "sha256:")+"  "+stableAssetName()+"\n"),
			responseStatus: http.StatusOK,
			expectTag:      "",
			expectURL:      "",
			expectErr:      true,
			checksumBody:   strings.TrimPrefix(testSHA256DigestB, "sha256:") + "  " + stableAssetName() + "\n",
		},
		{
			name:           "Same version (no update)",
			responseBody:   `{"tag_name":"v0.0.0","html_url":"https://github.com/Dicklesworthstone/beads_viewer/releases/tag/v0.0.0"}`, // Assumes current > v0.0.0
			responseStatus: http.StatusOK,
			expectTag:      "",
			expectURL:      "",
			expectErr:      false,
		},
		{
			name:           "Rate limit (403)",
			responseBody:   `{"message": "rate limit exceeded"}`,
			responseStatus: http.StatusForbidden,
			expectTag:      "",
			expectURL:      "",
			expectErr:      true,
		},
		{
			name:           "Rate limit (429)",
			responseBody:   `{"message":"rate limit exceeded"}`,
			responseStatus: http.StatusTooManyRequests,
			expectTag:      "",
			expectURL:      "",
			expectErr:      true,
		},
		{
			name:           "Server error (500)",
			responseBody:   "",
			responseStatus: http.StatusInternalServerError,
			expectTag:      "",
			expectURL:      "",
			expectErr:      true,
		},
		{
			name:           "Invalid JSON",
			responseBody:   `{invalid json}`,
			responseStatus: http.StatusOK,
			expectTag:      "",
			expectURL:      "",
			expectErr:      true,
		},
		{
			name:           "Invalid release tag",
			responseBody:   `{"tag_name":"v99.0.0.1","html_url":"https://github.com/Dicklesworthstone/beads_viewer/releases/tag/v99.0.0.1"}`,
			responseStatus: http.StatusOK,
			expectTag:      "",
			expectURL:      "",
			expectErr:      true,
		},
		{
			name:           "Newer release missing platform asset",
			responseBody:   `{"tag_name":"v99.0.0","html_url":"https://github.com/Dicklesworthstone/beads_viewer/releases/tag/v99.0.0","assets":[{"name":"checksums.txt","browser_download_url":"https://github.com/Dicklesworthstone/beads_viewer/releases/download/v99.0.0/checksums.txt","size":256,"digest":"` + testSHA256DigestB + `","state":"uploaded"}]}`,
			responseStatus: http.StatusOK,
			expectTag:      "",
			expectURL:      "",
			expectErr:      true,
		},
		{
			name:           "Newer release missing checksum asset",
			responseBody:   `{"tag_name":"v99.0.0","html_url":"https://github.com/Dicklesworthstone/beads_viewer/releases/tag/v99.0.0","assets":[{"name":"` + stableAssetName() + `","browser_download_url":"https://github.com/Dicklesworthstone/beads_viewer/releases/download/v99.0.0/` + stableAssetName() + `","size":1024,"digest":"` + testSHA256DigestA + `","state":"uploaded"}]}`,
			responseStatus: http.StatusOK,
			expectTag:      "",
			expectURL:      "",
			expectErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/checksums" {
					w.WriteHeader(http.StatusOK)
					if _, err := w.Write([]byte(tt.checksumBody)); err != nil {
						t.Errorf("write checksum response: %v", err)
					}
					return
				}
				if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
					t.Errorf("Accept header = %q", got)
				}
				if got := r.Header.Get("X-GitHub-Api-Version"); got != githubAPIVersion {
					t.Errorf("X-GitHub-Api-Version header = %q", got)
				}
				w.WriteHeader(tt.responseStatus)
				if _, err := w.Write([]byte(tt.responseBody)); err != nil {
					t.Errorf("write response: %v", err)
				}
			}))
			defer server.Close()

			client := server.Client()
			client.Timeout = 1 * time.Second
			rewriteGitHubDownloadsToServer(t, client, server.URL)

			tag, url, err := checkForUpdates(client, server.URL)

			if (err != nil) != tt.expectErr {
				t.Errorf("checkForUpdates() error = %v, expectErr %v", err, tt.expectErr)
				return
			}

			if tag != tt.expectTag {
				t.Errorf("checkForUpdates() tag = %v, want %v", tag, tt.expectTag)
			}
			if url != tt.expectURL {
				t.Errorf("checkForUpdates() url = %v, want %v", url, tt.expectURL)
			}
		})
	}
}

// TestSetGitHubAuth_GitHubDomain verifies that GITHUB_TOKEN is sent as a
// Bearer token in the Authorization header only for GitHub domains (#117).
func TestSetGitHubAuth_GitHubDomain(t *testing.T) {
	tests := []struct {
		name     string
		envVar   string
		envVal   string
		url      string
		wantAuth string
	}{
		{"GITHUB_TOKEN set + GitHub URL", "GITHUB_TOKEN", "ghp_test123", "https://api.github.com/repos/foo/bar", "Bearer ghp_test123"},
		{"GH_TOKEN set + GitHub URL", "GH_TOKEN", "gho_fallback456", "https://api.github.com/repos/foo/bar", "Bearer gho_fallback456"},
		{"No token set + GitHub URL", "", "", "https://api.github.com/repos/foo/bar", ""},
		{"GITHUB_TOKEN set + non-GitHub URL", "GITHUB_TOKEN", "ghp_test123", "https://example.com/download", ""},
		{"GITHUB_TOKEN set + localhost URL", "GITHUB_TOKEN", "ghp_test123", "http://localhost:8080/test", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear both env vars first
			os.Unsetenv("GITHUB_TOKEN")
			os.Unsetenv("GH_TOKEN")
			if tt.envVar != "" {
				os.Setenv(tt.envVar, tt.envVal)
				defer os.Unsetenv(tt.envVar)
			}

			req, err := http.NewRequest(http.MethodGet, tt.url, nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}
			setGitHubAuth(req)

			gotAuth := req.Header.Get("Authorization")
			if gotAuth != tt.wantAuth {
				t.Errorf("Authorization header = %q, want %q", gotAuth, tt.wantAuth)
			}
		})
	}
}

// TestGitHubToken_Precedence verifies GITHUB_TOKEN takes precedence over GH_TOKEN.
func TestGitHubToken_Precedence(t *testing.T) {
	os.Setenv("GITHUB_TOKEN", "primary")
	os.Setenv("GH_TOKEN", "fallback")
	defer os.Unsetenv("GITHUB_TOKEN")
	defer os.Unsetenv("GH_TOKEN")

	tok := githubToken()
	if tok != "primary" {
		t.Errorf("githubToken() = %q, want %q (GITHUB_TOKEN should take precedence)", tok, "primary")
	}
}
