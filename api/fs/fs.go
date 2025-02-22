package fs

import (
	"context"
	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/registry"
	"io"
	"io/fs"
)

// Registry kinds for filesystem components
const (
	// System represents a filesystem directory mapping from registry
	System registry.Kind = "fs"

	Register = "fs.register"
	Delete   = "fs.unregister"

	RegisterDefault = "fs.default.register"
	DeleteDefault   = "fs.default.delete"

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
		Rename(oldname, newname string) error
		Mkdir(name string, perm fs.FileMode) error
	}

	// FS combines read and write operations
	FS interface {
		ReadFS
		WriteFS
	}

	Registry interface {
		GetFS(name string) (FS, error)
		GetDefaultFS() (FS, error)
	}
)

func WithContext(ctx context.Context, reg Registry) context.Context {
	return context.WithValue(ctx, ctxapi.FSRegistryCtx, reg)
}

func GetRegistry(ctx context.Context) Registry {
	if reg, ok := ctx.Value(ctxapi.FSRegistryCtx).(Registry); ok {
		return reg
	}

	return nil
}
