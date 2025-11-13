// Package retry provides retry interceptor configuration.
package retry

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "complete config",
			config: Config{
				Enabled:     true,
				MaxAttempts: 3,
			},
			wantErr: false,
		},
		{
			name: "enabled only",
			config: Config{
				Enabled: true,
			},
			wantErr: false,
		},
		{
			name: "disabled",
			config: Config{
				Enabled:     false,
				MaxAttempts: 5,
			},
			wantErr: false,
		},
		{
			name:    "default config",
			config:  Config{},
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

			var decoded Config
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config, decoded)
		})
	}
}

func TestOptions_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		options Options
		wantErr bool
	}{
		{
			name: "complete options",
			options: Options{
				MaxAttempts: 5,
				BackoffMs:   1000,
			},
			wantErr: false,
		},
		{
			name: "max attempts only",
			options: Options{
				MaxAttempts: 3,
			},
			wantErr: false,
		},
		{
			name: "backoff only",
			options: Options{
				BackoffMs: 500,
			},
			wantErr: false,
		},
		{
			name:    "zero values",
			options: Options{},
			wantErr: false,
		},
		{
			name: "large backoff",
			options: Options{
				MaxAttempts: 10,
				BackoffMs:   30000,
			},
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

			var decoded Options
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.options, decoded)
		})
	}
}
