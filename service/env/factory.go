package env

import (
	"fmt"

	"github.com/ponyruntime/pony/api/registry"
	env2 "github.com/ponyruntime/pony/api/service/env"
	"go.uber.org/zap"
)

type EnvStorageFactoryAPI interface {
	CreateMemoryEnvStorage(kind registry.Kind, cfg *env2.CreateMemoryEnvStorageConfig, log *zap.Logger) (*MemoryStorage, error)
}

type DefaultEnvStorageFactory struct{}

func NewDefaultEnvStorageFactory() *DefaultEnvStorageFactory {
	return &DefaultEnvStorageFactory{}
}

func (f *DefaultEnvStorageFactory) CreateMemoryEnvStorage(kind registry.Kind, cfg *env2.CreateMemoryEnvStorageConfig, log *zap.Logger) (*MemoryStorage, error) {
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
