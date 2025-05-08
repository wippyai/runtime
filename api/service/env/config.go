package env

import (
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
)

type StorageMemoryConfig struct {
	Name string `json:"name"`

	Kind registry.Kind `json:"kind"`

	Meta registry.Metadata `json:"meta"`

	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
}

// Validate checks if the configuration is valid.
func (c *StorageMemoryConfig) Validate() error {
	return nil
}

type StorageFileConfig struct {
	Name string `json:"name"`

	FileName string `json:"file_name"`
}

// Validate checks if the configuration is valid.
func (c *StorageFileConfig) Validate() error {
	return nil
}
