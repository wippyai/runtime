// SPDX-License-Identifier: MPL-2.0

// Package terminal provides terminal service configuration.
package terminal

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKindConstant(t *testing.T) {
	assert.Equal(t, "terminal.host", Host)
}

func TestHostConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  HostConfig
		wantErr bool
	}{
		{
			name: "hide logs enabled",
			config: HostConfig{
				HideLogs: true,
			},
			wantErr: false,
		},
		{
			name: "hide logs disabled",
			config: HostConfig{
				HideLogs: false,
			},
			wantErr: false,
		},
		{
			name:    "default config",
			config:  HostConfig{},
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

			var decoded HostConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config.HideLogs, decoded.HideLogs)
		})
	}
}

func TestHostConfig_Validate(t *testing.T) {
	config := HostConfig{}
	assert.NoError(t, config.Validate())

	config = HostConfig{HideLogs: true}
	assert.NoError(t, config.Validate())
}

func TestHostConfig_initDefaults(t *testing.T) {
	config := HostConfig{}
	config.initDefaults()

	assert.NotNil(t, config.Lifecycle)
}

func TestHostConfig_WithLifecycle(t *testing.T) {
	config := HostConfig{
		HideLogs: true,
	}

	err := config.Validate()
	require.NoError(t, err)

	data, err := json.Marshal(&config)
	require.NoError(t, err)

	var decoded HostConfig
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, config.HideLogs, decoded.HideLogs)
}

func TestHost(t *testing.T) {
	assert.Equal(t, "terminal.host", Host)
}
