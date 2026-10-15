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

func TestCloseoutSkillDocumentsSafeWorktreeDisposal(t *testing.T) {
	content, err := os.ReadFile("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	skill := string(content)
	for _, required := range []string{
		"you must ask the disposal questionnaire",
		"closeout fails or is incomplete",
		"ask or invoke cleanup",
		"Only after it reports a successful merged closeout",
		"you must ask this questionnaire",
		"with `closeout complete`",
		"Branch: <recorded-branch>",
		"Path:   <recorded-worktree-path>",
		"Yes (recommended/default) | No",
		"Yes/No questionnaire",
		"Require an explicit, unambiguous `Yes`",
		"On `No`, no response",
		"ambiguous response",
		"do not invoke cleanup",
		"no eligible recorded target is available",
		"skills/beads-hub-closeout/dispose.sh <recorded-branch>",
		"git wtrm <recorded-branch>",
		"git branch -d <recorded-branch>",
		"never the filesystem path",
		"Do not offer or remove the reference checkout",
		"an arbitrary worktree, or a",
		"worktree removal fails",
		"the worktree and local branch were retained",
		"local branch deletion fails after worktree removal",
		"the worktree was removed and the local branch was retained",
		"never deletes a remote branch",
		"the completed closeout remains completed",
	} {
		if !strings.Contains(skill, required) {
			t.Errorf("closeout skill is missing disposal guidance %q", required)
		}
	}
	lowerSkill := strings.ToLower(skill)
	for _, thirdPerson := range []string{"agent may", "agent must", "may offer", "offer this question"} {
		if strings.Contains(lowerSkill, thirdPerson) {
			t.Errorf("closeout skill uses third-person disposal wording: %q", thirdPerson)
		}
	}

	if strings.Contains(skill, "gwtrm") {
		t.Error("closeout skill relies on the gwtrm shell alias")
	}
	if strings.Index(skill, "Only after it reports a successful merged closeout") >
		strings.Index(skill, "skills/beads-hub-closeout/dispose.sh <recorded-branch>") {
		t.Error("closeout skill offers disposal before successful closeout")
	}
}

func TestDisposalUsesRecordedBranchAndDeletesOnlyAfterWorktreeRemoval(t *testing.T) {
	commands, output, err := runDisposal(t, "delivery-branch", nil)
	if err != nil {
		t.Fatalf("disposal failed: %v\n%s", err, output)
	}
	if want := []string{
		"check-ref-format refs/heads/delivery-branch",
		"wtrm delivery-branch",
		"branch -d delivery-branch",
	}; !sameStrings(commands, want) {
		t.Fatalf("disposal commands = %q, want %q", commands, want)
	}
	for _, command := range commands {
		if strings.Contains(command, "remote") || strings.Contains(command, "push") || strings.Contains(command, "-D") {
			t.Fatalf("disposal invoked remote or force deletion: %q", command)
		}
	}
}

func TestDisposalDoesNotDeleteBranchWhenWorktreeRemovalFails(t *testing.T) {
	commands, output, err := runDisposal(t, "delivery-branch", map[string]string{"WTRM_EXIT": "1"})
	if err == nil {
		t.Fatal("disposal unexpectedly succeeded after worktree removal failed")
	}
	if want := []string{
		"check-ref-format refs/heads/delivery-branch",
		"wtrm delivery-branch",
	}; !sameStrings(commands, want) {
		t.Fatalf("disposal commands = %q, want %q", commands, want)
	}
	if strings.Contains(string(output), "branch -d") || strings.Contains(strings.Join(commands, "\n"), "branch -d") {
		t.Fatal("disposal attempted local branch deletion after worktree removal failed")
	}
	if !strings.Contains(string(output), "worktree and local branch were retained") {
		t.Fatalf("disposal returned an unclear failure: %s", output)
	}
}

func TestDisposalReportsRetainedBranchWhenBranchDeletionFails(t *testing.T) {
	commands, output, err := runDisposal(t, "delivery-branch", map[string]string{"BRANCH_EXIT": "1"})
	if err == nil {
		t.Fatal("disposal unexpectedly succeeded after branch deletion failed")
	}
	if want := []string{
		"check-ref-format refs/heads/delivery-branch",
		"wtrm delivery-branch",
		"branch -d delivery-branch",
	}; !sameStrings(commands, want) {
		t.Fatalf("disposal commands = %q, want %q", commands, want)
	}
	if !strings.Contains(string(output), "worktree removed; local branch was retained") {
		t.Fatalf("disposal returned an unclear partial-success diagnostic: %s", output)
	}
}

func TestDisposalRejectsInvalidBranchBeforeDisposal(t *testing.T) {
	commands, output, err := runDisposal(t, "../delivery-branch", nil)
	if err == nil {
		t.Fatal("disposal unexpectedly accepted an invalid branch")
	}
	if want := []string{"check-ref-format refs/heads/../delivery-branch"}; !sameStrings(commands, want) {
		t.Fatalf("disposal commands = %q, want %q", commands, want)
	}
	if !strings.Contains(string(output), "recorded branch is invalid") {
		t.Fatalf("disposal returned an unclear invalid-branch diagnostic: %s", output)
	}
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
}

func runDisposal(t *testing.T, branch string, variables map[string]string) ([]string, []byte, error) {
	t.Helper()
	stubDir := t.TempDir()
	commandsFile := filepath.Join(stubDir, "commands")
	writeExecutable(t, filepath.Join(stubDir, "git"), `#!/bin/sh
printf '%s\n' "$*" >> "$COMMANDS_FILE"
case "$1" in
check-ref-format)
  case "$2" in *..*) exit 1 ;; esac
  exit 0
  ;;
wtrm) exit "${WTRM_EXIT:-0}" ;;
branch) exit "${BRANCH_EXIT:-0}" ;;
esac
exit 99
`)

	environment := []string{"PATH=" + stubDir + ":/usr/bin:/bin", "COMMANDS_FILE=" + commandsFile}
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "PATH=") {
			environment = append(environment, variable)
		}
	}
	for key, value := range variables {
		environment = append(environment, key+"="+value)
	}
	command := exec.Command("env", append(environment, "bash", "dispose.sh", branch)...)
	command.Dir = filepath.Dir("dispose.sh")
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	contents, readErr := os.ReadFile(commandsFile)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("reading disposal command log: %v", readErr)
	}
	if readErr != nil {
		contents = nil
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	return lines, output, err
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
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
