// SPDX-License-Identifier: MPL-2.0

//go:build !windows

package lock

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockFile(file *os.File, exclusive, wait bool) error {
	how := unix.LOCK_SH
	if exclusive {
		how = unix.LOCK_EX
	}
	if !wait {
		how |= unix.LOCK_NB
	}
	for {
		err := unix.Flock(int(file.Fd()), how)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return ErrWorkspaceBusy
		}
		return err
	}
}

func unlockFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
