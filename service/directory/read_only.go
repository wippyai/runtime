package directory

import (
	"fmt"
	"io/fs"

	fsapi "github.com/wippyai/runtime/api/fs"
)

// ReadOnlyFS wraps a standard fs.FS to provide a read-only implementation
// that satisfies additional filesystem interfaces.
type ReadOnlyFS struct {
	embed fs.FS
}

// NewReadOnlyFS creates a new ReadOnlyFS instance wrapping the provided fs.FS.
func NewReadOnlyFS(embed fs.FS) *ReadOnlyFS {
	return &ReadOnlyFS{embed: embed}
}

// Open opens the named file for reading.
// It implements the fs.FS interface.
func (r ReadOnlyFS) Open(name string) (fs.File, error) {
	return r.embed.Open(name)
}

// Stat returns file info for the named file.
func (r ReadOnlyFS) Stat(name string) (fs.FileInfo, error) {
	open, err := r.embed.Open(name)
	if err != nil {
		return nil, fmt.Errorf("ReadOnlyFS.Stat: %w", err)
	}
	defer func() { _ = open.Close() }()

	info, err := open.Stat()
	if err != nil {
		return nil, fmt.Errorf("ReadOnlyFS.Stat: %w", err)
	}
	return info, nil
}

// ReadDir reads the directory named by path and returns a list of directory entries.
// It implements directory reading capability similar to os.ReadDir.
func (r ReadOnlyFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return fs.ReadDir(r.embed, name)
}

// OpenFile opens the named file with the given flag and permission.
// Since this is a read-only filesystem, it ignores the flag and perm parameters
// and returns a read-only file wrapper.
// It implements part of the fsapi.FS interface.
func (r ReadOnlyFS) OpenFile(name string, _ int, _ fs.FileMode) (fsapi.File, error) {
	file, err := r.embed.Open(name)
	if err != nil {
		return nil, fmt.Errorf("ReadOnlyFS.Open: %w", err)
	}
	return readOnlyFile{file}, nil
}

// Remove always returns an error as it's not supported in a read-only filesystem.
// It implements part of the fsapi.FS interface.
func (r ReadOnlyFS) Remove(_ string) error {
	return fmt.Errorf("ReadOnlyFS.Remove: operation not supported")
}

// Mkdir always returns an error as it's not supported in a read-only filesystem.
// It implements part of the fsapi.FS interface.
func (r ReadOnlyFS) Mkdir(_ string, _ fs.FileMode) error {
	return fmt.Errorf("ReadOnlyFS.Mkdir: operation not supported")
}

// readOnlyFile wraps an fs.File to block write operations.
type readOnlyFile struct {
	fs.File
}

// Write always returns an error as it's not supported in a read-only file.
func (readOnlyFile) Write([]byte) (int, error) {
	return 0, fmt.Errorf("readOnlyFile.Write: operation not supported")
}

// Seek always returns an error as seeking is not supported in this implementation.
func (readOnlyFile) Seek(int64, int) (int64, error) {
	return 0, fmt.Errorf("readOnlyFile.Seek: operation not supported")
}

// Sync always returns an error as it's not supported in a read-only file.
func (readOnlyFile) Sync() error {
	return fmt.Errorf("readOnlyFile.Sync: operation not supported")
}
