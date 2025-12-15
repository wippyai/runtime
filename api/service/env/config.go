// Package env provides environment service configuration.
package env

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/registry"
)

const (
	StorageMemory registry.Kind = "env.storage.memory"
	StorageFile   registry.Kind = "env.storage.file"
	StorageOS     registry.Kind = "env.storage.os"
	StorageRouter registry.Kind = "env.storage.router"
	Variable      registry.Kind = "env.variable"
)

// MemoryStorageConfig provides configuration for in-memory environment variable storage.
type MemoryStorageConfig struct {
	Meta attrs.Bag `json:"meta"`
}

// FileStorageConfig provides configuration for file-based environment variable storage.
type FileStorageConfig struct {
	Meta       attrs.Bag `json:"meta"`
	FilePath   string    `json:"file_path"`
	AutoCreate bool      `json:"auto_create"`
	FileMode   uint32    `json:"file_mode,omitempty"`
	DirMode    uint32    `json:"dir_mode,omitempty"`
}

// OSStorageConfig provides configuration for OS environment variable storage.
type OSStorageConfig struct {
	Meta attrs.Bag `json:"meta"`
}

// RouterStorageConfig provides configuration for routing environment variable requests across multiple storages.
type RouterStorageConfig struct {
	Meta     attrs.Bag `json:"meta"`
	Storages []string  `json:"storages"`
}

type Service interface {
	Add(ctx context.Context, entry registry.Entry) error
	Update(ctx context.Context, entry registry.Entry) error
	Delete(ctx context.Context, entry registry.Entry) error
}

type Manager interface {
	Service
	GetStorage(id registry.ID) (env.Storage, bool)
	ListStorages() map[registry.ID]env.Storage
}

func (c *MemoryStorageConfig) Validate() error {
	return nil
}

func (c *FileStorageConfig) Validate() error {
	if c.FilePath == "" {
		return env.ErrEmptyFilePath
	}
	return nil
}

func (c *OSStorageConfig) Validate() error {
	return nil
}

func (c *RouterStorageConfig) Validate() error {
	if len(c.Storages) == 0 {
		return env.ErrEmptyStorageList
	}
	return nil
}
