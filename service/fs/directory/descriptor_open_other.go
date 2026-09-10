//go:build !unix && !windows

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"errors"
	"io/fs"

	fsapi "github.com/wippyai/runtime/api/fs"
)

// OpenDescriptorAt fails closed on platforms without an atomic descriptor-root
// primitive. Falling back to a pathname would silently lose descriptor identity.
func (d *FS) OpenDescriptorAt(_ fs.File, name string, _ fsapi.DescriptorOpenRequest) (fsapi.File, error) {
	return nil, &fs.PathError{Op: "open-at", Path: name, Err: errors.ErrUnsupported}
}
