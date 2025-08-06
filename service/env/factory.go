package env

import (
	"fmt"

	"github.com/ponyruntime/pony/api/env"

	"github.com/ponyruntime/pony/api/registry"
	envservice "github.com/ponyruntime/pony/api/service/env"
	"go.uber.org/zap"
)

type StorageFactoryAPI interface {
	CreateMemoryEnvStorage(kind registry.Kind, cfg *envservice.CreateMemoryEnvStorageConfig, log *zap.Logger) (*MemoryStorage, error)
	CreateFileEnvStorage(kind registry.Kind, cfg *envservice.CreateFileEnvStorageConfig, log *zap.Logger) (*FileStorage, error)
	CreateOSEnvStorage(kind registry.Kind, cfg *envservice.CreateOSEnvStorageConfig, log *zap.Logger) (*OSStorage, error)
	CreateRouterEnvStorage(kind registry.Kind, cfg *envservice.CreateRouterEnvStorageConfig, storages map[registry.ID]env.Storage, log *zap.Logger) (*RouterStorage, error)
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

func (f *DefaultEnvStorageFactory) CreateOSEnvStorage(_ registry.Kind, cfg *envservice.CreateOSEnvStorageConfig, log *zap.Logger) (*OSStorage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration cannot be nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return NewOSStorage(log), nil
}

func (f *DefaultEnvStorageFactory) CreateRouterEnvStorage(_ registry.Kind, cfg *envservice.CreateRouterEnvStorageConfig, allStorages map[registry.ID]env.Storage, log *zap.Logger) (*RouterStorage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration cannot be nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	selectedStorages := make([]env.Storage, 0)

	for _, storageName := range cfg.Storages {
		storageID := registry.ParseID(storageName)
		storage, ok := allStorages[storageID]
		if !ok {
			return nil, fmt.Errorf("storage not found: %s", storageName)
		}
		selectedStorages = append(selectedStorages, storage)
	}

	// Create an empty router storage for now
	// In the real implementation, we would resolve the storage references here
	return NewRouterStorage(selectedStorages, log)
}
