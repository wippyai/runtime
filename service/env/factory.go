package env

import (
	"fmt"

	"github.com/ponyruntime/pony/api/registry"
	envservice "github.com/ponyruntime/pony/api/service/env"
	"go.uber.org/zap"
)

type EnvStorageFactoryAPI interface {
	CreateMemoryEnvStorage(kind registry.Kind, cfg *envservice.CreateMemoryEnvStorageConfig, log *zap.Logger) (*MemoryStorage, error)
	CreateFileEnvStorage(kind registry.Kind, cfg *envservice.CreateFileEnvStorageConfig, log *zap.Logger) (*FileStorage, error)
}

type DefaultEnvStorageFactory struct{}

func NewDefaultEnvStorageFactory() *DefaultEnvStorageFactory {
	return &DefaultEnvStorageFactory{}
}

func (f *DefaultEnvStorageFactory) CreateMemoryEnvStorage(_ registry.Kind, cfg *envservice.CreateMemoryEnvStorageConfig, log *zap.Logger) (*MemoryStorage, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	defaultValues := map[string]string{
		"OPENAI_API_KEY": "OPENAI_API_KEY-test",
		"OPENAI_API_URL": "OPENAI_API_URL-test",
	}

	storage := NewMemoryStorage(defaultValues, log)

	return storage, nil
}

func (f *DefaultEnvStorageFactory) CreateFileEnvStorage(_ registry.Kind, cfg *envservice.CreateFileEnvStorageConfig, log *zap.Logger) (*FileStorage, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	storage := NewFileStorage(cfg.FilePath, log)

	return storage, nil
}
