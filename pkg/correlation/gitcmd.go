package correlation

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

const (
	gitBatchMaxOutputBytes = 64 << 20
	gitBatchMaxStderrBytes = 64 << 10
)

type boundedGitStderr struct {
	data      []byte
	truncated bool
}

func (s *boundedGitStderr) Write(p []byte) (int, error) {
	remaining := gitBatchMaxStderrBytes - len(s.data)
	if remaining > 0 {
		s.data = append(s.data, p[:min(remaining, len(p))]...)
	}
	if len(p) > remaining {
		s.truncated = true
	}
	return len(p), nil
}

func (s *boundedGitStderr) String() string {
	message := strings.TrimSpace(string(s.data))
	if s.truncated {
		return message + " [stderr truncated]"
	}
	return message
}

type gitBatchProcessError struct {
	cause  error
	stderr string
}

func (e *gitBatchProcessError) Error() string {
	if e.stderr == "" {
		return e.cause.Error()
	}
	return fmt.Sprintf("%v: %s", e.cause, e.stderr)
}

func (e *gitBatchProcessError) Unwrap() error { return e.cause }

type gitBatchOutputLimitError struct {
	limit  int
	stderr string
}

func (e *gitBatchOutputLimitError) Error() string {
	message := fmt.Sprintf("Git batch output exceeds %d bytes", e.limit)
	if e.stderr != "" {
		message += ": " + e.stderr
	}
	return message
}

// runGitOutputBounded captures one batch Git process without allowing stdout
// or stderr to grow without bound. It always reaps a started process, including
// when the output limit or a read failure terminates the stream early.
func runGitOutputBounded(cmd *exec.Cmd) ([]byte, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("opening Git batch stdout: %w", err)
	}
	stderr := &boundedGitStderr{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		return nil, err
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, gitBatchMaxOutputBytes+1))
	if len(output) > gitBatchMaxOutputBytes {
		_ = cmd.Process.Kill()
		_ = stdout.Close()
		_ = cmd.Wait()
		return nil, &gitBatchOutputLimitError{limit: gitBatchMaxOutputBytes, stderr: stderr.String()}
	}
	if readErr != nil {
		_ = cmd.Process.Kill()
		_ = stdout.Close()
		waitErr := cmd.Wait()
		if waitErr != nil {
			readErr = fmt.Errorf("%w (process: %v)", readErr, waitErr)
		}
		return nil, &gitBatchProcessError{cause: readErr, stderr: stderr.String()}
	}
	if waitErr := cmd.Wait(); waitErr != nil {
		return nil, &gitBatchProcessError{cause: waitErr, stderr: stderr.String()}
	}
	return output, nil
}

// gitCommand returns an exec.Cmd for the git binary bound to ctx (issue #166).
// When ctx is cancelled — for example when the --robot-triage history
// prologue's timeout fires — any in-flight git subprocess is killed instead of
// leaking unbounded work. A nil ctx degrades to context.Background() (no
// cancellation), preserving the behavior of legacy constructors that never
// attach a context.
func gitCommand(ctx context.Context, args ...string) *exec.Cmd {
	if ctx == nil {
		ctx = context.Background()
	}
	return exec.CommandContext(ctx, "git", args...)
}
