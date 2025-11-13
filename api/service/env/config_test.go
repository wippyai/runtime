// Package env provides environment service configuration.
package env

import (
	"encoding/json"
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKindConstants(t *testing.T) {
	tests := []struct {
		name     string
		kind     registry.Kind
		expected string
	}{
		{"storage memory", KindStorageMemory, "env.storage.memory"},
		{"storage file", KindStorageFile, "env.storage.file"},
		{"storage os", KindStorageOS, "env.storage.os"},
		{"storage router", KindStorageRouter, "env.storage.router"},
		{"variable", KindVariable, "env.variable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.kind))
		})
	}
}

func TestMemoryStorageConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  MemoryStorageConfig
		wantErr bool
	}{
		{
			name: "with metadata",
			config: MemoryStorageConfig{
				Meta: registry.Metadata{"type": "memory"},
			},
			wantErr: false,
		},
		{
			name:    "empty config",
			config:  MemoryStorageConfig{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded MemoryStorageConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config, decoded)
		})
	}
}

func TestMemoryStorageConfig_Validate(t *testing.T) {
	config := MemoryStorageConfig{}
	assert.NoError(t, config.Validate())
}

func TestFileStorageConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  FileStorageConfig
		wantErr bool
	}{
		{
			name: "complete config",
			config: FileStorageConfig{
				Meta:       registry.Metadata{"type": "file"},
				FilePath:   "/etc/env.json",
				AutoCreate: true,
				FileMode:   0644,
				DirMode:    0755,
			},
			wantErr: false,
		},
		{
			name: "minimal config",
			config: FileStorageConfig{
				FilePath: "/tmp/env",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded FileStorageConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config, decoded)
		})
	}
}

func TestFileStorageConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  FileStorageConfig
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  FileStorageConfig{FilePath: "/etc/env"},
			wantErr: false,
		},
		{
			name:    "empty filepath",
			config:  FileStorageConfig{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOSStorageConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  OSStorageConfig
		wantErr bool
	}{
		{
			name: "with metadata",
			config: OSStorageConfig{
				Meta: registry.Metadata{"type": "os"},
			},
			wantErr: false,
		},
		{
			name:    "empty config",
			config:  OSStorageConfig{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded OSStorageConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config, decoded)
		})
	}
}

func TestOSStorageConfig_Validate(t *testing.T) {
	config := OSStorageConfig{}
	assert.NoError(t, config.Validate())
}

func TestRouterStorageConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  RouterStorageConfig
		wantErr bool
	}{
		{
			name: "with storages",
			config: RouterStorageConfig{
				Meta:     registry.Metadata{"type": "router"},
				Storages: []string{"storage1", "storage2"},
			},
			wantErr: false,
		},
		{
			name: "single storage",
			config: RouterStorageConfig{
				Storages: []string{"storage1"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded RouterStorageConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config, decoded)
		})
	}
}

func TestRouterStorageConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  RouterStorageConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid config",
			config:  RouterStorageConfig{Storages: []string{"s1"}},
			wantErr: false,
		},
		{
			name:    "no storages",
			config:  RouterStorageConfig{},
			wantErr: true,
			errMsg:  "must have at least one storage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
