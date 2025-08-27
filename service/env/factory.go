package env

import (
	"fmt"
	"os"

	envsvc "github.com/ponyruntime/pony/api/service/env"

	"github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

type StorageFactory interface {
	CreateMemoryEnvStorage(cfg *envsvc.MemoryStorageConfig, log *zap.Logger) (*MemoryStorage, error)
	CreateFileEnvStorage(cfg *envsvc.FileStorageConfig, log *zap.Logger) (*FileStorage, error)
	CreateOSEnvStorage(cfg *envsvc.OSStorageConfig, log *zap.Logger) (*OSStorage, error)
	CreateRouterEnvStorage(cfg *envsvc.RouterStorageConfig, storages map[registry.ID]env.Storage, log *zap.Logger) (*RouterStorage, error)
}

type DefaultEnvStorageFactory struct{}

func NewDefaultEnvStorageFactory() *DefaultEnvStorageFactory {
	return &DefaultEnvStorageFactory{}
}

func (f *DefaultEnvStorageFactory) CreateMemoryEnvStorage(cfg *envsvc.MemoryStorageConfig, log *zap.Logger) (*MemoryStorage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration cannot be nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return NewMemoryStorage(nil, log), nil
}

func (f *DefaultEnvStorageFactory) CreateFileEnvStorage(cfg *envsvc.FileStorageConfig, log *zap.Logger) (*FileStorage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration cannot be nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	fileMode := os.FileMode(0644)
	if cfg.FileMode > 0 {
		fileMode = os.FileMode(cfg.FileMode)
	}

	dirMode := os.FileMode(0755)
	if cfg.DirMode > 0 {
		dirMode = os.FileMode(cfg.DirMode)
	}

	return NewFileStorage(cfg.FilePath, cfg.AutoCreate, fileMode, dirMode, log), nil
}

func (f *DefaultEnvStorageFactory) CreateOSEnvStorage(cfg *envsvc.OSStorageConfig, log *zap.Logger) (*OSStorage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration cannot be nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return NewOSStorage(log), nil
}

func (f *DefaultEnvStorageFactory) CreateRouterEnvStorage(cfg *envsvc.RouterStorageConfig, allStorages map[registry.ID]env.Storage, log *zap.Logger) (*RouterStorage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration cannot be nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	selectedStorages := make([]env.Storage, 0, len(cfg.Storages))
	for _, storageName := range cfg.Storages {
		storageID := registry.ParseID(storageName)
		storage, ok := allStorages[storageID]
		if !ok {
			return nil, fmt.Errorf("storage not found: %s", storageName)
		}
		selectedStorages = append(selectedStorages, storage)
	}

	return NewRouterStorage(selectedStorages, log)
}
