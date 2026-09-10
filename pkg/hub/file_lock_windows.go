//go:build windows

package hub

import (
	"os"

	"golang.org/x/sys/windows"
)

func lockLedgerFile(file *os.File) error {
	handle := windows.Handle(file.Fd())
	var overlapped windows.Overlapped
	return windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped)
}

func unlockLedgerFile(file *os.File) error {
	handle := windows.Handle(file.Fd())
	var overlapped windows.Overlapped
	return windows.UnlockFileEx(handle, 0, 1, 0, &overlapped)
}
