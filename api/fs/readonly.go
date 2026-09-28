// SPDX-License-Identifier: MPL-2.0

package fs

import (
	"io"
	"io/fs"
	"os"
	"time"
)

var _ FS = (*ReadOnlyFS)(nil)

// ReadOnlyFS adapts an fs.ReadDirFS to the FS interface.
// All write operations return ErrReadOnly.
type ReadOnlyFS struct {
	fsys fs.ReadDirFS
}

// NewReadOnlyFS creates a read-only filesystem adapter.
func NewReadOnlyFS(fsys fs.ReadDirFS) *ReadOnlyFS {
	return &ReadOnlyFS{fsys: fsys}
}

// Open opens a file for reading.
func (r *ReadOnlyFS) Open(name string) (fs.File, error) {
	file, err := r.fsys.Open(name)
	if err != nil {
		return nil, err
	}
	return &readOnlyFile{File: file}, nil
}

// OpenFile implements WriteFS.OpenFile for read-only access.
// Returns ErrReadOnly for write modes.
func (r *ReadOnlyFS) OpenFile(name string, flag int, _ fs.FileMode) (File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0 {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  ErrReadOnly,
		}
	}

	file, err := r.fsys.Open(name)
	if err != nil {
		return nil, err
	}

	return &readOnlyFile{File: file}, nil
}

// Remove implements WriteFS.Remove.
// Always returns ErrReadOnly.
func (r *ReadOnlyFS) Remove(name string) error {
	return &fs.PathError{
		Op:   "remove",
		Path: name,
		Err:  ErrReadOnly,
	}
}

// Mkdir implements WriteFS.Mkdir.
// Always returns ErrReadOnly.
func (r *ReadOnlyFS) Mkdir(name string, _ fs.FileMode) error {
	return &fs.PathError{
		Op:   "mkdir",
		Path: name,
		Err:  ErrReadOnly,
	}
}

// ReadDir reads a directory.
func (r *ReadOnlyFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return r.fsys.ReadDir(name)
}

// Stat implements ReadFS.Stat.
func (r *ReadOnlyFS) Stat(name string) (fs.FileInfo, error) {
	if statFS, ok := r.fsys.(fs.StatFS); ok {
		return statFS.Stat(name)
	}
	file, err := r.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return file.Stat()
}

// Lstat implements ReadFS.Lstat.
// Falls back to Stat if the underlying FS does not support Lstat.
func (r *ReadOnlyFS) Lstat(name string) (fs.FileInfo, error) {
	type lstater interface {
		Lstat(name string) (fs.FileInfo, error)
	}
	if ls, ok := r.fsys.(lstater); ok {
		return ls.Lstat(name)
	}
	return r.Stat(name)
}

// Rename implements WriteFS.Rename.
// Always returns ErrReadOnly.
func (r *ReadOnlyFS) Rename(oldname, _ string) error {
	return &fs.PathError{
		Op:   "rename",
		Path: oldname,
		Err:  ErrReadOnly,
	}
}

// Truncate implements WriteFS.Truncate.
// Always returns ErrReadOnly.
func (r *ReadOnlyFS) Truncate(name string, _ int64) error {
	return &fs.PathError{
		Op:   "truncate",
		Path: name,
		Err:  ErrReadOnly,
	}
}

// Chtimes implements WriteFS.Chtimes.
// Always returns ErrReadOnly.
func (r *ReadOnlyFS) Chtimes(name string, _, _ time.Time) error {
	return &fs.PathError{
		Op:   "chtimes",
		Path: name,
		Err:  ErrReadOnly,
	}
}

// Close closes the underlying filesystem when it owns resources.
func (r *ReadOnlyFS) Close() error {
	if closer, ok := r.fsys.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// readOnlyFile wraps an fs.File to block write operations.
type readOnlyFile struct {
	fs.File
}

// Write returns ErrReadOnly.
func (f *readOnlyFile) Write([]byte) (int, error) {
	return 0, ErrReadOnly
}

// Seek changes the read offset.
func (f *readOnlyFile) Seek(offset int64, whence int) (int64, error) {
	seeker, ok := f.File.(io.Seeker)
	if !ok {
		return 0, fs.ErrInvalid
	}
	return seeker.Seek(offset, whence)
}

// Sync is a no-op for read-only files.
func (f *readOnlyFile) Sync() error {
	return nil
}

// ReadDir reads directory entries.
func (f *readOnlyFile) ReadDir(n int) ([]fs.DirEntry, error) {
	dir, ok := f.File.(fs.ReadDirFile)
	if !ok {
		return nil, fs.ErrInvalid
	}
	return dir.ReadDir(n)
}
