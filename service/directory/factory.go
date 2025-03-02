package directory

import (
	fsapi "github.com/ponyruntime/pony/api/fs"
	"io/fs"
)

// FSFactoryAPI defines the interface for creating filesystem instances
type FSFactoryAPI interface {
	// CreateFS creates a new filesystem instance
	CreateFS(dirPath string, mode fs.FileMode) (fsapi.FS, error)
}

// FSFactory implements FSFactoryAPI to create directory-based filesystems
type FSFactory struct{}

// NewDirectoryFSFactory creates a new factory instance for directory filesystems
func NewDirectoryFSFactory() *FSFactory {
	return &FSFactory{}
}

// CreateFS creates a new directory filesystem
func (f *FSFactory) CreateFS(dirPath string, mode fs.FileMode) (fsapi.FS, error) {
	return NewDirectoryFS(dirPath, mode)
}
