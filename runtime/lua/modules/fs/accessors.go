// SPDX-License-Identifier: MPL-2.0

package fs

import fsapi "github.com/wippyai/runtime/api/fs"

// Backend returns the underlying filesystem so sibling modules (e.g. archive)
// can open files against the same FS a Lua handle wraps.
func (f *FS) Backend() fsapi.FS { return f.fs }

// Resolve turns a path into one resolved against this handle's working
// directory, matching how the fs methods resolve their arguments.
func (f *FS) Resolve(p string) (string, error) { return f.resolvePath(p) }

// Backend returns the underlying file so sibling modules can use it as a
// seekable source (the directory backend's file is an io.ReaderAt).
func (f *File) Backend() fsapi.File { return f.file }
