package env

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/env"

	"github.com/ponyruntime/pony/api/registry"
)

const (
	KindStorageMemory registry.Kind = "env.storage.memory"
	KindStorageFile   registry.Kind = "env.storage.file"
	KindStorageOS     registry.Kind = "env.storage.os"
	KindStorageRouter registry.Kind = "env.storage.router"
	KindVariable      registry.Kind = "env.variable"
)

type MemoryStorageConfig struct {
	Meta registry.Metadata `json:"meta"`
}

type FileStorageConfig struct {
	Meta       registry.Metadata `json:"meta"`
	FilePath   string            `json:"file_path"`
	AutoCreate bool              `json:"auto_create"`
	FileMode   int               `json:"file_mode,omitempty"`
	DirMode    int               `json:"dir_mode,omitempty"`
}

type OSStorageConfig struct {
	Meta registry.Metadata `json:"meta"`
}

type RouterStorageConfig struct {
	Meta     registry.Metadata `json:"meta"`
	Storages []string          `json:"storages"`
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
		return env.ErrInvalidVariableName
	}
	return nil
}

func (c *OSStorageConfig) Validate() error {
	return nil
}

func (c *RouterStorageConfig) Validate() error {
	if len(c.Storages) == 0 {
		return fmt.Errorf("router storage must have at least one storage")
	}
	return nil
}
