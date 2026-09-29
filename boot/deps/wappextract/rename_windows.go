// SPDX-License-Identifier: MPL-2.0

//go:build windows

package wappextract

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

var (
	osRename            = os.Rename
	renameRetryTimeout  = 5 * time.Second
	renameRetryMaxDelay = 250 * time.Millisecond
)

// RenameDir renames a directory. Windows denies a directory rename while any
// process, such as an antivirus or indexing service, holds a handle to a file
// in the tree, so transient sharing errors are retried for a bounded time.
func RenameDir(oldpath, newpath string) error {
	deadline := time.Now().Add(renameRetryTimeout)
	delay := 10 * time.Millisecond
	for {
		err := osRename(oldpath, newpath)
		if err == nil || !isTransientRenameError(err) || time.Now().Add(delay).After(deadline) {
			return err
		}
		time.Sleep(delay)
		delay = min(delay*2, renameRetryMaxDelay)
	}
}

func isTransientRenameError(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
