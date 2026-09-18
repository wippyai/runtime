//go:build !linux && !darwin

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	fsapi "github.com/wippyai/runtime/api/fs"
	"io/fs"
)

func (d *FS) WriteFileAtomic(_ string, _ []byte, _ fs.FileMode) error {
	return fsapi.ErrAtomicWriteUnsupported
}
