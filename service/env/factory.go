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
	CreateOSEnvStorage(cfg *envsvc.OSStorageConfig, log *zap.Logger) (env.Storage, error)
	CreateRouterEnvStorage(cfg *envsvc.RouterStorageConfig, storages map[registry.ID]env.Storage, log *zap.Logger) (*RouterStorage, error)
}

type DefaultEnvStorageFactory struct {
	// staticEnv holds predefined environment variables that replace OS environment access.
	// When set, CreateOSEnvStorage returns StaticStorage instead of OSStorage.
	staticEnv map[string]string
}

type FactoryOption func(*DefaultEnvStorageFactory)

// WithStaticEnv configures the factory to return StaticStorage instead of OSStorage
// when CreateOSEnvStorage is called. This effectively replaces OS environment variable
// access with a predefined static set of key-value pairs.
//
// This is useful for:
//   - Testing environments where you want controlled, reproducible env vars
//   - Sandboxed execution where OS env access should be restricted
//   - Scenarios where env vars need to be predefined and immutable
//
// When staticEnv is provided, all OS storage requests will use these static values
// instead of reading from the actual operating system environment.
func WithStaticEnv(env map[string]string) FactoryOption {
	return func(f *DefaultEnvStorageFactory) {
		f.staticEnv = env
	}
}

func NewDefaultEnvStorageFactory(opts ...FactoryOption) *DefaultEnvStorageFactory {
	f := &DefaultEnvStorageFactory{}
	for _, opt := range opts {
		opt(f)
	}
	return f
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

// CreateOSEnvStorage creates environment storage for OS-level environment variables.
//
// IMPORTANT: When the factory is configured with WithStaticEnv option, this method
// returns StaticStorage instead of OSStorage. This means that instead of reading from
// the actual operating system environment, it returns a read-only storage backed by
// the predefined static environment map.
//
// Behavior:
//   - With staticEnv: Returns StaticStorage with predefined key-value pairs (cuts out OS access)
//   - Without staticEnv: Returns OSStorage that reads from actual OS environment variables
//
// This design allows transparent replacement of OS environment access with controlled
// static values without changing any calling code or storage interfaces.
func (f *DefaultEnvStorageFactory) CreateOSEnvStorage(cfg *envsvc.OSStorageConfig, log *zap.Logger) (env.Storage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration cannot be nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// If static env is configured, return StaticStorage instead of OSStorage.
	// This effectively replaces OS environment variable access with predefined values.
	if f.staticEnv != nil {
		return NewStaticStorage(f.staticEnv, log), nil
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
