package env

import (
	envservice "github.com/ponyruntime/pony/api/service/env"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"testing"
)

func TestDefaultEnvStorageFactory_CreateMemoryEnvStorage(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *envservice.MemoryStorageConfig
		wantErr bool
	}{
		{
			name: "valid configuration",
			cfg: &envservice.MemoryStorageConfig{
				Meta: registry.Metadata{},
			},
			wantErr: false,
		},
		{
			name:    "nil configuration",
			cfg:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := NewDefaultEnvStorageFactory()
			logger := zap.NewNop()

			storage, err := factory.CreateMemoryEnvStorage(tt.cfg, logger)

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
		cfg     *envservice.FileStorageConfig
		wantErr bool
	}{
		{
			name: "valid configuration",
			cfg: &envservice.FileStorageConfig{
				FilePath: "/tmp/test.env",
			},
			wantErr: false,
		},
		{
			name:    "nil configuration",
			cfg:     nil,
			wantErr: true,
		},
		{
			name: "empty file path",
			cfg: &envservice.FileStorageConfig{
				FilePath: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := NewDefaultEnvStorageFactory()
			logger := zap.NewNop()

			storage, err := factory.CreateFileEnvStorage(tt.cfg, logger)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, storage)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, storage)
				assert.IsType(t, &FileStorage{}, storage)

				// Verify storage was created successfully
				assert.NotNil(t, storage)
			}
		})
	}
}
