// Package exec provides process execution service.
package exec

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKindConstants(t *testing.T) {
	assert.Equal(t, "exec.native", KindNativeExecutor)
}

func TestProcessOptions_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		options ProcessOptions
		wantErr bool
	}{
		{
			name: "complete options",
			options: ProcessOptions{
				WorkDir: "/var/app",
				Env: map[string]string{
					"PATH": "/usr/bin",
					"HOME": "/home/user",
				},
			},
			wantErr: false,
		},
		{
			name: "workdir only",
			options: ProcessOptions{
				WorkDir: "/tmp",
			},
			wantErr: false,
		},
		{
			name: "env only",
			options: ProcessOptions{
				Env: map[string]string{"VAR": "value"},
			},
			wantErr: false,
		},
		{
			name:    "empty options",
			options: ProcessOptions{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.options)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded ProcessOptions
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.options, decoded)
		})
	}
}

func TestNativeExecutorConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  NativeExecutorConfig
		wantErr bool
	}{
		{
			name: "complete config",
			config: NativeExecutorConfig{
				DefaultWorkDir: "/var/app",
				DefaultEnv: map[string]string{
					"PATH": "/usr/local/bin:/usr/bin",
					"HOME": "/root",
				},
				CommandWhitelist: []string{"/bin/sh", "/usr/bin/python"},
			},
			wantErr: false,
		},
		{
			name: "with whitelist only",
			config: NativeExecutorConfig{
				CommandWhitelist: []string{"/bin/bash"},
			},
			wantErr: false,
		},
		{
			name: "minimal config",
			config: NativeExecutorConfig{
				DefaultWorkDir: "/tmp",
			},
			wantErr: false,
		},
		{
			name:    "empty config",
			config:  NativeExecutorConfig{},
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

			var decoded NativeExecutorConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config, decoded)
		})
	}
}
