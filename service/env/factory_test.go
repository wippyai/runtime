package env

import (
	"github.com/ponyruntime/pony/api/supervisor"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	envservice "github.com/ponyruntime/pony/api/service/env"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestDefaultEnvStorageFactory_CreateMemoryEnvStorage(t *testing.T) {
	tests := []struct {
		name    string
		kind    registry.Kind
		cfg     *envservice.CreateMemoryEnvStorageConfig
		wantErr bool
	}{
		{
			name: "valid configuration",
			kind: registry.Kind("test"),
			cfg: &envservice.CreateMemoryEnvStorageConfig{
				Name: "",
				Kind: "",
				Meta: nil,
				Lifecycle: supervisor.LifecycleConfig{
					StartTimeout: time.Second,
					StopTimeout:  time.Second,
				}},
			wantErr: false,
		},
		{
			name:    "nil configuration",
			kind:    registry.Kind("test"),
			cfg:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := NewDefaultEnvStorageFactory()
			logger := zap.NewNop()

			storage, err := factory.CreateMemoryEnvStorage(tt.kind, tt.cfg, logger)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, storage)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, storage)
				assert.IsType(t, &MemoryStorage{}, storage)
			}
		})
	}
}

func TestDefaultEnvStorageFactory_CreateFileEnvStorage(t *testing.T) {
	tests := []struct {
		name    string
		kind    registry.Kind
		cfg     *envservice.CreateFileEnvStorageConfig
		wantErr bool
	}{
		{
			name: "valid configuration",
			kind: registry.Kind("test"),
			cfg: &envservice.CreateFileEnvStorageConfig{
				FilePath: "/tmp/test.env",
			},
			wantErr: false,
		},
		{
			name:    "nil configuration",
			kind:    registry.Kind("test"),
			cfg:     nil,
			wantErr: true,
		},
		{
			name: "empty file path",
			kind: registry.Kind("test"),
			cfg: &envservice.CreateFileEnvStorageConfig{
				FilePath: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := NewDefaultEnvStorageFactory()
			logger := zap.NewNop()

			storage, err := factory.CreateFileEnvStorage(tt.kind, tt.cfg, logger)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, storage)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, storage)
				assert.IsType(t, &FileStorage{}, storage)

				// Verify file path is set correctly
				assert.Equal(t, tt.cfg.FilePath, storage.filepath)
			}
		})
	}
}
