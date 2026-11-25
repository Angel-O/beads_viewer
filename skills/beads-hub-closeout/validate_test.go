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

func TestCommitCloseoutUsesSelectedReferenceCommitAndFailsClosed(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		target     string
		checkout   string
		linkFails  bool
		wantLinked bool
	}{
		{name: "older accepted target", target: "target", checkout: "reference", wantLinked: true},
		{name: "unreachable target", target: "unreachable", checkout: "reference"},
		{name: "wrong checkout", target: "target", checkout: "other"},
		{name: "privacy failure", target: "private", checkout: "reference"},
		{name: "link failure", target: "target", checkout: "reference", linkFails: true, wantLinked: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repository, targetSHA := closeoutRepository(t, scenario.target, scenario.checkout)
			commitArgument := targetSHA
			if scenario.name == "older accepted target" {
				commitArgument = strings.ToUpper(commitArgument)
			}
			commands, output, err := runCommitCloseout(t, repository, commitArgument, "reference", scenario.linkFails)
			if scenario.wantLinked && scenario.linkFails {
				if err == nil || !strings.Contains(string(output), "Hub correlation failed") {
					t.Fatalf("closeout should stop on link failure: %v\n%s", err, output)
				}
				if !containsCommand(commands, "link synthetic-item "+targetSHA) || containsCommand(commands, "close ") {
					t.Fatalf("link failure commands = %q, want exact link and no close", commands)
				}
				return
			}
			if scenario.wantLinked {
				if err != nil {
					t.Fatalf("closeout failed: %v\n%s", err, output)
				}
				if !containsCommand(commands, "link synthetic-item "+targetSHA) || !containsCommand(commands, "close synthetic-item --reason Integrated and correlated --json") {
					t.Fatalf("closeout commands = %q, want selected SHA linked before close", commands)
				}
				if !strings.Contains(string(output), targetSHA) {
					t.Fatalf("success did not report selected commit: %s", output)
				}
				return
			}
			if err == nil || containsCommand(commands, "link ") || containsCommand(commands, "close ") {
				t.Fatalf("rejected delivery was not fail-closed: err=%v commands=%q output=%s", err, commands, output)
			}
		})
	}
}

func TestCommitCloseoutRejectsInvalidSHABeforeHubCommands(t *testing.T) {
	repository, _ := closeoutRepository(t, "target", "reference")
	commands, output, err := runCommitCloseout(t, repository, strings.Repeat("f", 40), "reference", false)
	if err == nil || !strings.Contains(string(output), "integration commit is not available") || len(commands) != 0 {
		t.Fatalf("invalid commit was not rejected before Hub commands: err=%v commands=%q output=%s", err, commands, output)
	}
}

func closeoutRepository(t *testing.T, selected, checkout string) (string, string) {
	t.Helper()
	repository := t.TempDir()
	git(t, repository, "init", "--quiet", "--initial-branch=reference")
	git(t, repository, "config", "user.name", "Synthetic Test")
	git(t, repository, "config", "user.email", "synthetic@example.invalid")
	commit(t, repository, "docs: synthetic baseline")
	git(t, repository, "branch", "baseline")
	commit(t, repository, "docs: synthetic target")
	targetSHA, err := exec.Command("git", "-C", repository, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	targetSHA = []byte(strings.TrimSpace(string(targetSHA)))
	if selected == "private" {
		commit(t, repository, "ctx:synthetic-private-target")
		targetSHA, err = exec.Command("git", "-C", repository, "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatal(err)
		}
		targetSHA = []byte(strings.TrimSpace(string(targetSHA)))
		commit(t, repository, "docs: newer than selected target")
	} else {
		commit(t, repository, "docs: newer than selected target")
	}
	if selected == "unreachable" {
		git(t, repository, "checkout", "--quiet", "baseline")
		commit(t, repository, "docs: unreachable target")
		targetSHA, err = exec.Command("git", "-C", repository, "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatal(err)
		}
		targetSHA = []byte(strings.TrimSpace(string(targetSHA)))
		git(t, repository, "checkout", "--quiet", "reference")
	}
	if checkout == "other" {
		git(t, repository, "branch", "other")
		git(t, repository, "checkout", "--quiet", "other")
	}
	return repository, string(targetSHA)
}

func runCommitCloseout(t *testing.T, repository, target, branch string, linkFails bool) ([]string, []byte, error) {
	t.Helper()
	scriptDirectory := t.TempDir()
	for _, name := range []string{"closeout.sh", "validate.sh"} {
		contents, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(scriptDirectory, name), contents, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	stubDir := t.TempDir()
	commandsFile := filepath.Join(stubDir, "commands")
	writeExecutable(t, filepath.Join(stubDir, "wbd"), `#!/bin/sh
printf '%s\n' "$*" >> "$COMMANDS_FILE"
case "$1" in
  show) printf '[{"id":"synthetic-item","issue_type":"task","labels":["ctx:synthetic-context"],"status":"in_progress"}]\n'; exit 0 ;;
  link) exit "${LINK_EXIT:-0}" ;;
  close) exit 0 ;;
esac
exit 99
`)
	writeExecutable(t, filepath.Join(stubDir, "bd"), "#!/bin/sh\nprintf '{\"key\":\"issue_prefix\",\"schema_version\":1,\"value\":\"synthetic\"}\\n'\n")
	path, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("jq is required by closeout validation")
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".local", "share", "beads", "hub", ".beads"), 0o700); err != nil {
		t.Fatal(err)
	}
	environment := append([]string(nil), os.Environ()...)
	environment = append(environment,
		"PATH="+stubDir+":"+filepath.Dir(path)+":/usr/bin:/bin",
		"HOME="+home,
		"COMMANDS_FILE="+commandsFile,
		"LINK_EXIT="+map[bool]string{true: "1", false: "0"}[linkFails],
	)
	command := exec.Command("bash", filepath.Join(scriptDirectory, "closeout.sh"), "synthetic-item", "--commit", target, branch)
	command.Dir = repository
	command.Env = environment
	output, runErr := command.CombinedOutput()
	contents, readErr := os.ReadFile(commandsFile)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	return lines, output, runErr
}

func containsCommand(commands []string, prefix string) bool {
	for _, command := range commands {
		if strings.HasPrefix(command, prefix) {
			return true
		}
	}
	return false
}

func TestPRCloseoutStillCorrelatesMergedCommit(t *testing.T) {
	repository, mergeSHA := closeoutRepository(t, "target", "reference")
	remote := filepath.Join(t.TempDir(), "origin.git")
	if output, err := exec.Command("git", "init", "--bare", "--initial-branch=reference", remote).CombinedOutput(); err != nil {
		t.Fatalf("creating local remote: %v\n%s", err, output)
	}
	git(t, repository, "remote", "add", "origin", remote)
	git(t, repository, "push", "--quiet", "origin", "reference")
	scriptDirectory := t.TempDir()
	for _, name := range []string{"closeout.sh", "validate.sh"} {
		contents, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(scriptDirectory, name), contents, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	stubDir := t.TempDir()
	commandsFile := filepath.Join(stubDir, "commands")
	ghResponse := "{\"state\":\"MERGED\",\"mergedAt\":\"2026-01-01T00:00:00Z\",\"mergeCommit\":{\"oid\":\"" + string(mergeSHA) + "\"},\"baseRefName\":\"reference\"}"
	writeExecutable(t, filepath.Join(stubDir, "gh"), "#!/bin/sh\nprintf '%s\\n' '"+ghResponse+"'\n")
	writeExecutable(t, filepath.Join(stubDir, "wbd"), `#!/bin/sh
printf '%s\n' "$*" >> "$COMMANDS_FILE"
case "$1" in
  show) printf '[{"id":"synthetic-item","issue_type":"task","labels":["ctx:synthetic-context"],"status":"in_progress"}]\n'; exit 0 ;;
  link|close) exit 0 ;;
esac
exit 99
`)
	jqPath, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("jq is required by closeout validation")
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".local", "share", "beads", "hub", ".beads"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(stubDir, "bd"), "#!/bin/sh\nprintf '%s\\n' '{\"key\":\"issue_prefix\",\"schema_version\":1,\"value\":\"synthetic\"}'\n")
	environment := append([]string(nil), os.Environ()...)
	environment = append(environment, "PATH="+stubDir+":"+filepath.Dir(jqPath)+":/usr/bin:/bin", "HOME="+home, "COMMANDS_FILE="+commandsFile)
	command := exec.Command("bash", filepath.Join(scriptDirectory, "closeout.sh"), "synthetic-item", "https://github.com/example/repo/pull/1")
	command.Dir = repository
	command.Env = environment
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("PR closeout failed: %v\n%s", err, output)
	}
	commands, err := os.ReadFile(commandsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(commands), "link synthetic-item "+string(mergeSHA)) || !strings.Contains(string(commands), "close synthetic-item --reason Merged and correlated --json") || !strings.Contains(string(output), "closeout complete: merge "+string(mergeSHA)+" on base branch reference; item closed.") {
		t.Fatalf("PR closeout did not preserve merged-SHA correlation: %s", commands)
	}
}

func TestCloseoutSkillDoesNotOwnCleanup(t *testing.T) {
	content, err := os.ReadFile("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	skill := string(content)
	for _, ownedCleanup := range []string{"questionnaire", "dispose.sh", "git pull", "shared-handoff mode"} {
		if strings.Contains(skill, ownedCleanup) {
			t.Errorf("closeout skill must not own cleanup coordination %q", ownedCleanup)
		}
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
