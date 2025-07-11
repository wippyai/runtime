package env

import (
	"fmt"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
)

type CreateMemoryEnvStorageConfig struct {
	Name      string                     `json:"name"`
	Kind      registry.Kind              `json:"kind"`
	Meta      registry.Metadata          `json:"meta"`
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
}

func (c *CreateMemoryEnvStorageConfig) Validate() error {
	return nil
}

type CreateFileEnvStorageConfig struct {
	Name      string                     `json:"name"`
	Kind      registry.Kind              `json:"kind"`
	Meta      registry.Metadata          `json:"meta"`
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
	FilePath  string                     `json:"file_path"`
}

func (c *CreateFileEnvStorageConfig) Validate() error {
	if c.FilePath == "" {
		return fmt.Errorf("file path cannot be empty")
	}
	return nil
}

type CreateOSEnvStorageConfig struct {
	Name      string                     `json:"name"`
	Kind      registry.Kind              `json:"kind"`
	Meta      registry.Metadata          `json:"meta"`
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
}

func (c *CreateOSEnvStorageConfig) Validate() error {
	return nil
}
