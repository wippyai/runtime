package filesystem

import (
	"context"
	"io"
	"io/fs"

	"github.com/ponyruntime/pony/api/payload"
)

// todo: root fs

type (
	// AttributeReader provides access to file attributes.
	AttributeReader interface {
		Attributes(ctx context.Context, name string) (payload.Payload, error)
	}

	// AttributeWriter allows modifying file attributes.
	AttributeWriter interface {
		SetAttributes(ctx context.Context, name string, attrs payload.Payload) error
	}

	// Writer provides basic file writing operations.
	Writer interface {
		Create(ctx context.Context, name string) (WritableFile, error)
		WriteFile(ctx context.Context, name string, data []byte) error
	}

	// DirOps handles directory operations.
	DirOps interface {
		Mkdir(ctx context.Context, name string, perm fs.FileMode) error
		MkdirAll(ctx context.Context, path string) error
	}

	// FileOps handles file operations.
	FileOps interface {
		Remove(ctx context.Context, name string) error
		RemoveAll(ctx context.Context, name string) error
		Rename(ctx context.Context, oldname, newname string) error
	}

	// TempOps handles temporary file and directory operations.
	TempOps interface {
		TempFile(ctx context.Context, dir, pattern string) (WritableFile, error)
		TempDir(ctx context.Context, dir, pattern string) (string, error)
	}

	// WritableFile extends fs.File with write operations.
	// Note: Locking has been removed here in favor of resource-level control.
	WritableFile interface {
		fs.File
		io.Writer
		io.Seeker
		Sync() error
	}

	// FileSystem composes all operations into a complete interface.
	FileSystem interface {
		fs.FS
		fs.StatFS
		fs.ReadDirFS
		AttributeReader
		AttributeWriter
		Writer
		DirOps
		FileOps
	}
)
