// Package fs provides filesystem abstractions and a registry for managing
// multiple filesystem instances.
package fs

import (
	"io"
	"io/fs"
	"time"

	"github.com/wippyai/runtime/api/event"
)

// System identifies the fs system in the event bus.
const System event.System = "fs"

// Event kinds for filesystem operations.
const (
	FsRegister event.Kind = "fs.register"
	FsDelete   event.Kind = "fs.delete"
	FsAccept   event.Kind = "fs.accept"
	FsReject   event.Kind = "fs.reject"
)

type (
	// ReadFS uses the standard fs.FS family of interfaces to provide read-only
	// filesystem operations.
	ReadFS interface {
		fs.FS
		fs.StatFS
		fs.ReadDirFS
		// Lstat returns file info without following symlinks.
		Lstat(name string) (fs.FileInfo, error)
	}

	// File represents a readable and writable file with additional
	// seeking and synchronization capabilities.
	File interface {
		fs.File
		io.Writer
		io.Seeker
		// Sync commits the current contents of the file to stable storage.
		Sync() error
	}

	// WriteFS adds write operations to filesystem, extending the standard
	// fs interfaces with write capabilities.
	WriteFS interface {
		// OpenFile opens the named file with specified flag and permissions.
		OpenFile(name string, flag int, perm fs.FileMode) (File, error)
		// Remove deletes the named file or directory.
		Remove(name string) error
		// Mkdir creates a new directory with the specified name and permission bits.
		Mkdir(name string, perm fs.FileMode) error
		// Rename moves or renames a file within the filesystem.
		Rename(oldname, newname string) error
		// Truncate changes the size of the named file.
		Truncate(name string, size int64) error
		// Chtimes changes the access and modification times of the named file.
		Chtimes(name string, atime, mtime time.Time) error
	}

	// FS combines read and write operations into a complete filesystem interface.
	FS interface {
		ReadFS
		WriteFS
	}

	// Registry provides access to named filesystem instances.
	Registry interface {
		// GetFS retrieves a filesystem by name.
		// Returns the filesystem and a boolean indicating if it was found.
		GetFS(name string) (FS, bool)
	}
)
