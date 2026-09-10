// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"io/fs"

	fsapi "github.com/wippyai/runtime/api/fs"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
)

// CreateFSConfig is a config for CreateFS.
type CreateFSConfig struct {
	DirPath  string
	Mode     fs.FileMode
	AutoInit bool
	ReadOnly bool
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
	if cfg.ReadOnly && cfg.AutoInit {
		return nil, dirapi.ErrReadOnlyAutoInit
	}

	filesystem, err := NewFS(cfg.DirPath, cfg.Mode, cfg.AutoInit)
	if err != nil {
		return nil, err
	}
	if cfg.ReadOnly {
		return fsapi.NewReadOnlyFS(filesystem), nil
	}
	return filesystem, nil
}
