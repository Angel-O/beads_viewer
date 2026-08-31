package closeout_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testIssuePrefix = "team-hub"

func TestMetadataValidationUsesRepositoryRefsAndHistory(t *testing.T) {
	t.Run("allows ID-free reviewed range", func(t *testing.T) {
		repository := syntheticRepository(t, "docs: harden closeout validation")
		commit(t, repository, "docs: safe target change")
		git(t, repository, "tag", "validation-fixture")
		if output, err := runMetadataValidation(repository, "refs/heads/baseline", "HEAD"); err != nil {
			t.Fatalf("metadata validation failed: %v\n%s", err, output)
		}
	})

	t.Run("ignores unrelated local branch and accepted baseline", func(t *testing.T) {
		repository := syntheticRepository(t, "docs: accepted baseline with ctx:synthetic-baseline")
		git(t, repository, "branch", "feature/global-synthetic-unrelated")
		commit(t, repository, "docs: safe target change")
		if output, err := runMetadataValidation(repository, "refs/heads/baseline", "HEAD"); err != nil {
			t.Fatalf("unrelated metadata blocked target range: %v\n%s", err, output)
		}
	})

	t.Run("checks the supplied fetched range", func(t *testing.T) {
		repository := syntheticRepository(t, "docs: synthetic root")
		git(t, repository, "checkout", "--quiet", "-b", "target")
		commit(t, repository, "docs: safe target change ctx:synthetic-fetched")
		git(t, repository, "checkout", "--quiet", "main")
		output, err := runMetadataValidation(repository, "refs/heads/baseline", "refs/heads/target")
		if err == nil {
			t.Fatal("metadata validation accepted a private pattern in the supplied range")
		}
		if strings.Contains(string(output), "ctx:synthetic-fetched") {
			t.Fatalf("validator exposed matched metadata: %s", output)
		}
	})

	tests := []struct {
		name  string
		leak  string
		setup func(*testing.T, string, string)
	}{
		{
			name: "active branch",
			leak: "feature/team-hub-synthetic",
			setup: func(t *testing.T, repository, leak string) {
				git(t, repository, "checkout", "--quiet", "-b", leak)
				commit(t, repository, "docs: safe target change")
			},
		},
		{
			name: "tag on reviewed range",
			leak: "team-hub-synthetic-tag",
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
			leak: "team-hub-synthetic-body",
			setup: func(t *testing.T, repository, leak string) {
				commit(t, repository, "docs: synthetic body\n\nprivate marker "+leak)
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			repository := syntheticRepository(t, "docs: synthetic root")
			testCase.setup(t, repository, testCase.leak)
			output, err := runMetadataValidation(repository, "refs/heads/baseline", "HEAD")
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

func TestCloseoutRequiresFullPullRequestURLBeforeCommands(t *testing.T) {
	for _, selector := range []string{"123", "owner/repository#123", "https://github.com/owner/repository/pull/123/"} {
		t.Run(selector, func(t *testing.T) {
			stubDir := t.TempDir()
			calledFile := filepath.Join(stubDir, "called")
			for _, command := range []string{"gh", "git", "wbd"} {
				writeExecutable(t, filepath.Join(stubDir, command), "#!/bin/sh\nprintf '%s\\n' \"$0\" >> \"$CALLED_FILE\"\nexit 99\n")
			}

			closeout := exec.Command("bash", "closeout.sh", "private-item", selector)
			closeout.Dir = filepath.Dir("closeout.sh")
			closeout.Env = append(os.Environ(), "PATH="+stubDir+":/usr/bin:/bin", "CALLED_FILE="+calledFile)
			output, err := closeout.CombinedOutput()
			if err == nil {
				t.Fatal("closeout unexpectedly accepted an invalid pull request selector")
			}
			if exitError, ok := err.(*exec.ExitError); !ok || exitError.ExitCode() != 2 {
				t.Fatalf("closeout returned %v, want exit code 2\n%s", err, output)
			}
			if !strings.Contains(string(output), "full GitHub pull-request URL") {
				t.Fatalf("closeout returned an unclear diagnostic: %s", output)
			}
			if _, err := os.Stat(calledFile); err == nil {
				t.Fatalf("closeout invoked a command before rejecting selector: %s", calledFile)
			} else if !os.IsNotExist(err) {
				t.Fatalf("checking command marker: %v", err)
			}
		})
	}
}

func TestCloseoutPassesFullPullRequestURLUnchangedToGH(t *testing.T) {
	calledFile := filepath.Join(t.TempDir(), "gh-args")
	ghDir := t.TempDir()
	writeExecutable(t, filepath.Join(ghDir, "gh"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$GH_ARGS_FILE\"\nexit 1\n")

	selector := "https://github.com/octo-org/example_repo/pull/42"
	closeout := exec.Command("bash", "closeout.sh", "private-item", selector)
	closeout.Env = append(os.Environ(), "PATH="+ghDir+":/usr/bin:/bin", "GH_ARGS_FILE="+calledFile)
	output, err := closeout.CombinedOutput()
	if err == nil {
		t.Fatal("closeout unexpectedly completed with a failing gh stub")
	}
	if !strings.Contains(string(output), "cannot read pull request") {
		t.Fatalf("closeout did not preserve gh failure handling: %s", output)
	}
	arguments, err := os.ReadFile(calledFile)
	if err != nil {
		t.Fatalf("reading gh arguments: %v", err)
	}
	argumentLines := strings.Split(strings.TrimSpace(string(arguments)), "\n")
	if len(argumentLines) < 3 || argumentLines[2] != selector {
		t.Fatalf("gh received %q, want URL unchanged", arguments)
	}
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
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

func runMetadataValidation(repository, base, tip string) ([]byte, error) {
	command := exec.Command("bash", "validate.sh", "--metadata-only", repository, testIssuePrefix, base, tip)
	return command.CombinedOutput()
}
