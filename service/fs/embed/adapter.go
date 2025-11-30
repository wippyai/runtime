package embed

import (
	"io/fs"
	"os"

	fsapi "github.com/wippyai/runtime/api/fs"
)

var _ fsapi.FS = (*ReadOnlyFS)(nil)

// ReadOnlyFS adapts an fs.ReadDirFS (from pack) to fsapi.FS interface.
// All write operations return fs.ErrPermission.
type ReadOnlyFS struct {
	fs.ReadDirFS
}

// NewReadOnlyFS creates a read-only filesystem adapter.
func NewReadOnlyFS(fsys fs.ReadDirFS) fsapi.FS {
	return &ReadOnlyFS{ReadDirFS: fsys}
}

// OpenFile implements fsapi.WriteFS.OpenFile for read-only access.
// Returns fs.ErrPermission for write modes.
func (r *ReadOnlyFS) OpenFile(name string, flag int, _ fs.FileMode) (fsapi.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0 {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  fs.ErrPermission,
		}
	}

	file, err := r.Open(name)
	if err != nil {
		return nil, err
	}

	return &readOnlyFile{File: file}, nil
}

type readOnlyFile struct {
	fs.File
}

func (f *readOnlyFile) Write([]byte) (int, error) {
	return 0, fs.ErrPermission
}

func (f *readOnlyFile) Seek(int64, int) (int64, error) {
	return 0, fs.ErrPermission
}

func (f *readOnlyFile) Sync() error {
	return nil
}

// Remove implements fsapi.WriteFS.Remove.
// Always returns fs.ErrPermission.
func (r *ReadOnlyFS) Remove(name string) error {
	return &fs.PathError{
		Op:   "remove",
		Path: name,
		Err:  fs.ErrPermission,
	}
}

// Mkdir implements fsapi.WriteFS.Mkdir.
// Always returns fs.ErrPermission.
func (r *ReadOnlyFS) Mkdir(name string, _ fs.FileMode) error {
	return &fs.PathError{
		Op:   "mkdir",
		Path: name,
		Err:  fs.ErrPermission,
	}
}

// Stat implements fsapi.ReadFS.Stat by delegating to the embedded fs.StatFS.
func (r *ReadOnlyFS) Stat(name string) (fs.FileInfo, error) {
	if statFS, ok := r.ReadDirFS.(fs.StatFS); ok {
		return statFS.Stat(name)
	}
	file, err := r.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return file.Stat()
}
