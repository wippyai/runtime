// SPDX-License-Identifier: MPL-2.0

// Package exec provides process execution service.
package exec

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
)

func TestKindConstants(t *testing.T) {
	assert.Equal(t, "exec.native", NativeExecutor)
}

func TestProcessOptions_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		options ProcessOptions
		name    string
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

func TestNativeExecutorConfig_Validate(t *testing.T) {
	t.Run("empty config is valid", func(t *testing.T) {
		cfg := &NativeExecutorConfig{}
		assert.NoError(t, cfg.Validate())
	})

	t.Run("complete config is valid", func(t *testing.T) {
		cfg := &NativeExecutorConfig{
			DefaultWorkDir:   "/tmp",
			DefaultEnv:       map[string]string{"PATH": "/bin"},
			CommandWhitelist: []string{"echo"},
		}
		assert.NoError(t, cfg.Validate())
	})
}

func TestDockerExecutorConfig_Validate(t *testing.T) {
	t.Run("empty image returns error", func(t *testing.T) {
		cfg := &DockerExecutorConfig{}
		err := cfg.Validate()
		assert.ErrorIs(t, err, ErrImageRequired)
	})

	t.Run("with image is valid", func(t *testing.T) {
		cfg := &DockerExecutorConfig{Image: "alpine:latest"}
		assert.NoError(t, cfg.Validate())
	})

	t.Run("complete config is valid", func(t *testing.T) {
		cfg := &DockerExecutorConfig{
			Image:           "ubuntu:22.04",
			Host:            "unix:///var/run/docker.sock",
			DefaultWorkDir:  "/app",
			NetworkMode:     "bridge",
			MemoryLimit:     512 * 1024 * 1024,
			CPUQuota:        100000,
			AutoRemove:      true,
			ReadOnlyRootfs:  true,
			NoNewPrivileges: true,
			CapDrop:         []string{"ALL"},
			CapAdd:          []string{"NET_BIND_SERVICE"},
			PidsLimit:       100,
		}
		assert.NoError(t, cfg.Validate())
	})
}

func TestProcessWaitCommandID(t *testing.T) {
	assert.Equal(t, dispatcher.CommandID(150), ProcessWait)
}

func TestProcessWaitCmd(t *testing.T) {
	cmd := AcquireProcessWaitCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, ProcessWait, cmd.CmdID())
	cmd.Release()

	cmd2 := AcquireProcessWaitCmd()
	assert.Nil(t, cmd2.Process)
	cmd2.Release()
}

func TestProcessWaitResponse(t *testing.T) {
	resp := ProcessWaitResponse{
		ExitCode: 0,
		Error:    nil,
	}
	assert.Equal(t, 0, resp.ExitCode)
	assert.Nil(t, resp.Error)

	resp2 := ProcessWaitResponse{
		ExitCode: 1,
		Error:    errors.New("process failed"),
	}
	assert.Equal(t, 1, resp2.ExitCode)
	assert.NotNil(t, resp2.Error)
}

func TestErrImageRequired(t *testing.T) {
	assert.Contains(t, ErrImageRequired.Error(), "docker image is required")
}
