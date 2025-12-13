package directory

import (
	"io/fs"

	fsapi "github.com/wippyai/runtime/api/fs"
)

// CreateFSConfig is a config for CreateFS.
type CreateFSConfig struct {
	DirPath  string
	Mode     fs.FileMode
	AutoInit bool
}

// FactoryAPI defines the interface for creating filesystem instances.
type FactoryAPI interface {
	// CreateFS creates a new filesystem instance.
	CreateFS(cfg CreateFSConfig) (fsapi.FS, error)
}

// Factory implements FactoryAPI to create directory-based filesystems.
type Factory struct{}

// NewFactory creates a new factory instance for directory filesystems.
func NewFactory() *Factory {
	return &Factory{}
}

// CreateFS creates a new directory filesystem.
func (f *Factory) CreateFS(cfg CreateFSConfig) (fsapi.FS, error) {
	return NewDirectoryFS(cfg.DirPath, cfg.Mode, cfg.AutoInit)
}
