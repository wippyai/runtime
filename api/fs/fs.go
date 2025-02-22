package fs

import (
	"context"
	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"io"
	"io/fs"
)

// Registry kinds for filesystem components
const (
	// System represents a filesystem directory mapping from registry
	System events.System = "fs"

	Register = "fs.register"
	Delete   = "fs.delete"

	RegisterDefault = "fs.register_default"
	DeleteDefault   = "fs.delete_default"

	Accept = "fs.accept"
	Reject = "fs.reject"
)

type (
	// ReadFS uses the standard fs.FS family of interfaces
	ReadFS interface {
		fs.FS
		fs.StatFS
		fs.ReadDirFS
	}

	// File represents a readable and writable file
	File interface {
		fs.File
		io.Writer
		io.Seeker
	}

	// WriteFS adds write operations to filesystem
	WriteFS interface {
		OpenFile(name string, flag int, perm fs.FileMode) (File, error)
		Remove(name string) error
		Mkdir(name string, perm fs.FileMode) error
	}

	// FS combines read and write operations
	FS interface {
		ReadFS
		WriteFS
	}

	Registry interface {
		GetFS(name string) (FS, bool)
		GetDefaultFS() (FS, bool)
	}
)

func WithContext(ctx context.Context, reg Registry) context.Context {
	return context.WithValue(ctx, ctxapi.FSRegistryCtx, reg)
}

func FromContext(ctx context.Context) Registry {
	if reg, ok := ctx.Value(ctxapi.FSRegistryCtx).(Registry); ok {
		return reg
	}

	return nil
}
