//go:build !unix && !windows

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"errors"
	"io/fs"

	fsapi "github.com/wippyai/runtime/api/fs"
)

// These platforms are deliberately fail-closed until their retained-handle
// mutation primitive is implemented. A pathname fallback would reintroduce the
// descriptor replacement race this interface exists to eliminate.
func (*FS) CreateDirectoryAt(fs.File, string, fs.FileMode) error { return errors.ErrUnsupported }
func (*FS) RenameAt(fs.File, string, fs.File, string) error      { return errors.ErrUnsupported }
func (*FS) UnlinkFileAt(fs.File, string) error                   { return errors.ErrUnsupported }
func (*FS) RemoveDirectoryAt(fs.File, string) error              { return errors.ErrUnsupported }

var _ fsapi.DescriptorMutator = (*FS)(nil)
