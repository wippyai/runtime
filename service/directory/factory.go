package directory

import (
	"io/fs"

	fsapi "github.com/wippyai/runtime/api/fs"
)

// CreateFSConfig is a config for CreateFS.
type CreateFSConfig struct {
	Name     string
	DirPath  string
	Mode     fs.FileMode
	AutoInit bool
}

// FSFactoryAPI defines the interface for creating filesystem instances
type FSFactoryAPI interface {
	// CreateFS creates a new filesystem instance
	CreateFS(cfg CreateFSConfig) (fsapi.FS, error)
}

// FSFactory implements FSFactoryAPI to create directory-based filesystems
type FSFactory struct{}

// NewDirectoryFSFactory creates a new factory instance for directory filesystems
func NewDirectoryFSFactory() *FSFactory {
	return &FSFactory{}
}

// CreateFS creates a new directory filesystem
func (f *FSFactory) CreateFS(cfg CreateFSConfig) (fsapi.FS, error) {
	return NewDirectoryFS(cfg.DirPath, cfg.Mode, cfg.AutoInit)
}
