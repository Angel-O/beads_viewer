//go:build !windows

package agents

import (
	"os"

	"golang.org/x/sys/unix"
)

func openAgentFileForMutation(path string) (*os.File, error) {
	return os.Open(path)
}

func lockAgentFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX)
}

func unlockAgentFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
