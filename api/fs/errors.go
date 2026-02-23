// SPDX-License-Identifier: MPL-2.0

package fs

import "errors"

var (
	ErrClosed           = errors.New("filesystem is closed")
	ErrPermissionDenied = errors.New("permission denied")
	ErrInvalidFileMode  = errors.New("invalid file mode: contains bits outside of fs.ModePerm")
)
