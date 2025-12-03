// Package fs provides filesystem abstractions and a registry for managing
// multiple filesystem instances.
package fs

import (
	"io"
	"io/fs"

	"github.com/wippyai/runtime/api/event"
)

// System identifies the fs system in the event bus.
const System event.System = "fs"

// Event kinds for filesystem operations.
const (
	Register event.Kind = "fs.register"
	Delete   event.Kind = "fs.delete"
	Accept   event.Kind = "fs.accept"
	Reject   event.Kind = "fs.reject"
)

type (
	// ReadFS uses the standard fs.FS family of interfaces to provide read-only
	// filesystem operations.
	ReadFS interface {
		fs.FS
		fs.StatFS
		fs.ReadDirFS
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
