package env

import (
	"fmt"

	"github.com/ponyruntime/pony/api/registry"
	envservice "github.com/ponyruntime/pony/api/service/env"
	"go.uber.org/zap"
)

//nolint:revive // ok for now
type EnvStorageFactoryAPI interface {
	CreateMemoryEnvStorage(kind registry.Kind, cfg *envservice.CreateMemoryEnvStorageConfig, log *zap.Logger) (*MemoryStorage, error)
	CreateFileEnvStorage(kind registry.Kind, cfg *envservice.CreateFileEnvStorageConfig, log *zap.Logger) (*FileStorage, error)
}

type DefaultEnvStorageFactory struct{}

func NewDefaultEnvStorageFactory() *DefaultEnvStorageFactory {
	return &DefaultEnvStorageFactory{}
}

func (f *DefaultEnvStorageFactory) CreateMemoryEnvStorage(_ registry.Kind, cfg *envservice.CreateMemoryEnvStorageConfig, log *zap.Logger) (*MemoryStorage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration cannot be nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	storage := NewMemoryStorage(nil, log)

	return storage, nil
}

func (f *DefaultEnvStorageFactory) CreateFileEnvStorage(_ registry.Kind, cfg *envservice.CreateFileEnvStorageConfig, log *zap.Logger) (*FileStorage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration cannot be nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	storage := NewFileStorage(cfg.FilePath, log)

	return storage, nil
}
