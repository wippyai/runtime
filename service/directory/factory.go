package directory

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	fsapi "github.com/ponyruntime/pony/api/fs"
	dirapi "github.com/ponyruntime/pony/api/service/directory"
	"github.com/ponyruntime/pony/embed"
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
	// Create the filesystem using the factory
	if cfg.Name == dirapi.TypeNameEmbed {
		dirPath := filepath.Clean(cfg.DirPath)
		dirPath = strings.TrimPrefix(dirPath, "./")

		if _, err := fs.Stat(embed.FS(), dirPath); err != nil {
			return nil, fmt.Errorf("embed stat: %w", err)
		}
		fsys, err := fs.Sub(embed.FS(), dirPath)
		if err != nil {
			return nil, err
		}
		return NewReadOnlyFS(fsys), nil
	}
	return NewDirectoryFS(cfg.DirPath, cfg.Mode, cfg.AutoInit)
}
