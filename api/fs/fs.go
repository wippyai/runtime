// Package fs provides filesystem abstractions and a registry for managing
// multiple filesystem instances.
package fs

import (
	"io"
	"io/fs"

	"github.com/ponyruntime/pony/api/event"
)

// Event bus constants for filesystem operations
const (
	// System represents the filesystem event bus system identifier
	System event.System = "fs"

	// Register is a command event to register a new filesystem
	Register event.Kind = "fs.register"
	// Delete is a command event to remove a filesystem from the registry
	Delete event.Kind = "fs.delete"

	// Accept is emitted when a filesystem command is successfully processed
	Accept event.Kind = "fs.accept"
	// Reject is emitted when a filesystem command fails
	Reject event.Kind = "fs.reject"
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
