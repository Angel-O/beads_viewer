package closeout_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetadataValidationUsesRepositoryRefsAndHistory(t *testing.T) {
	t.Run("allows ID-free reviewed range", func(t *testing.T) {
		repository := syntheticRepository(t, "docs: harden closeout validation")
		commit(t, repository, "docs: safe target change")
		git(t, repository, "tag", "validation-fixture")
		if output, err := runMetadataValidation(repository); err != nil {
			t.Fatalf("metadata validation failed: %v\n%s", err, output)
		}
	})

	t.Run("ignores unrelated local branch and accepted baseline", func(t *testing.T) {
		repository := syntheticRepository(t, "docs: accepted baseline with ctx:synthetic-baseline")
		git(t, repository, "branch", "feature/global-synthetic-unrelated")
		commit(t, repository, "docs: safe target change")
		if output, err := runMetadataValidation(repository); err != nil {
			t.Fatalf("unrelated metadata blocked target range: %v\n%s", err, output)
		}
	})

	tests := []struct {
		name  string
		leak  string
		setup func(*testing.T, string, string)
	}{
		{
			name: "active branch",
			leak: "feature/global-synthetic",
			setup: func(t *testing.T, repository, leak string) {
				git(t, repository, "checkout", "--quiet", "-b", leak)
				commit(t, repository, "docs: safe target change")
			},
		},
		{
			name: "tag on reviewed range",
			leak: "global-synthetic-tag",
			setup: func(t *testing.T, repository, leak string) {
				commit(t, repository, "docs: safe target change")
				git(t, repository, "tag", leak)
			},
		},
		{
			name: "new commit subject",
			leak: "ctx:synthetic-history",
			setup: func(t *testing.T, repository, leak string) {
				commit(t, repository, leak)
			},
		},
		{
			name: "new commit body",
			leak: "global-synthetic-body",
			setup: func(t *testing.T, repository, leak string) {
				commit(t, repository, "docs: synthetic body\n\nprivate marker "+leak)
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			repository := syntheticRepository(t, "docs: synthetic root")
			testCase.setup(t, repository, testCase.leak)
			output, err := runMetadataValidation(repository)
			if err == nil {
				t.Fatal("metadata validation accepted a synthetic private pattern")
			}
			if strings.Contains(string(output), testCase.leak) {
				t.Fatalf("validator exposed matched metadata: %s", output)
			}
			if !strings.Contains(string(output), "private Hub identity detected") {
				t.Fatalf("validator returned an unexpected diagnostic: %s", output)
			}
		})
	}
}

func syntheticRepository(t *testing.T, message string) string {
	t.Helper()
	repository := t.TempDir()
	git(t, repository, "init", "--quiet", "--initial-branch=main")
	git(t, repository, "config", "user.name", "Synthetic Test")
	git(t, repository, "config", "user.email", "synthetic@example.invalid")
	commit(t, repository, message)
	git(t, repository, "branch", "baseline")
	return repository
}

func commit(t *testing.T, repository, message string) {
	t.Helper()
	path := filepath.Join(repository, "fixture.txt")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(message + "\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	git(t, repository, "add", "fixture.txt")
	git(t, repository, "commit", "--quiet", "-m", message)
}

func git(t *testing.T, repository string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git command failed: %v\n%s", err, output)
	}
}

func runMetadataValidation(repository string) ([]byte, error) {
	command := exec.Command("bash", "validate.sh", "--metadata-only", repository, "refs/heads/baseline")
	return command.CombinedOutput()
}
