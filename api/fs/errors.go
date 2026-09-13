// SPDX-License-Identifier: MPL-2.0

package fs

import (
	"errors"
	"syscall"
)

var (
	ErrClosed           = errors.New("filesystem is closed")
	ErrPermissionDenied = errors.New("permission denied")
	ErrReadOnly         = error(syscall.EROFS)
	ErrInvalidFileMode  = errors.New("invalid file mode: contains bits outside of fs.ModePerm")
	// ErrNotDirectory preserves a known directory-type failure on platforms
	// where syscall.ENOTDIR aliases a generic missing-path error.
	ErrNotDirectory = errors.New("not a directory")
	// These preserve native failure details that broad io/fs error categories
	// cannot express consistently across platforms.
	ErrIsDirectory = errors.New("is a directory")
	ErrNotEmpty    = errors.New("directory is not empty")
	ErrBusy        = errors.New("filesystem resource is busy")
)

var (
	ErrAtomicWriteUnsupported = errors.New("atomic file publication is unsupported")
	ErrPublishedSyncFailed    = errors.New("file published but directory sync failed")
)
