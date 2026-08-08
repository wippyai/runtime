// SPDX-License-Identifier: MPL-2.0

//go:build windows

package artifact

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

var errLockBusy = errors.New("artifact lock is busy")

func tryLockFile(file *os.File) (func() error, error) {
	overlapped := &windows.Overlapped{}
	handle := windows.Handle(file.Fd())
	err := windows.LockFileEx(
		handle,
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		overlapped,
	)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return nil, errLockBusy
	}
	if err != nil {
		return nil, err
	}
	return func() error {
		return windows.UnlockFileEx(handle, 0, 1, 0, overlapped)
	}, nil
}
