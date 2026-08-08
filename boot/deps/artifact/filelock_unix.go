// SPDX-License-Identifier: MPL-2.0

//go:build !windows

package artifact

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

var errLockBusy = errors.New("artifact lock is busy")

func tryLockFile(file *os.File) (func() error, error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return nil, errLockBusy
	}
	if err != nil {
		return nil, err
	}
	return func() error {
		return unix.Flock(int(file.Fd()), unix.LOCK_UN)
	}, nil
}
