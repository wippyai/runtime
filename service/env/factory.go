package env

import (
	"fmt"

	"github.com/ponyruntime/pony/api/registry"
	env2 "github.com/ponyruntime/pony/api/service/env"
	"go.uber.org/zap"
)

type EnvStorageFactoryAPI interface {
	CreateMemoryEnvStorage(kind registry.Kind, cfg *env2.StorageMemoryConfig, log *zap.Logger) (*MemoryStorage, error)

	//CreateFileEnvStorage(kind registry.Kind, cfg *env2.StorageMemoryConfig) (*FileStorage, error)
}

type DefaultEnvStorageFactory struct{}

func NewDefaultEnvStorageFactory() *DefaultEnvStorageFactory {
	return &DefaultEnvStorageFactory{}
}

func (f *DefaultEnvStorageFactory) CreateMemoryEnvStorage(kind registry.Kind, cfg *env2.StorageMemoryConfig, log *zap.Logger) (*MemoryStorage, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	storage := NewMemoryStorage(nil, log)

	return storage, nil
}

//func (f *DefaultEnvStorageFactory) CreateFileEnvStorage() {
//}
