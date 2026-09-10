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
)
