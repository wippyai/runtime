// Package tokenstore provides token store service configuration.
package tokenstore

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
)

func TestConstants(t *testing.T) {
	t.Run("TokenTypeOpaque", func(t *testing.T) {
		assert.Equal(t, security.TokenType("opaque"), TokenTypeOpaque)
	})

	t.Run("TokenStore", func(t *testing.T) {
		assert.Equal(t, "security.token_store", TokenStore)
	})
}

func TestConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "complete config",
			config: Config{
				Store:             registry.NewID("stores", "tokens"),
				TokenLength:       32,
				TokenKey:          "secret-key",
				DefaultExpiration: 24 * time.Hour,
			},
			wantErr: false,
		},
		{
			name: "minimal config",
			config: Config{
				Store:       registry.NewID("s", "t"),
				TokenLength: 16,
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

			var decoded Config
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config.Store, decoded.Store)
			assert.Equal(t, tt.config.TokenLength, decoded.TokenLength)
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: Config{
				Store:       registry.NewID("s", "tokens"),
				TokenLength: 32,
			},
			wantErr: false,
		},
		{
			name: "missing store",
			config: Config{
				TokenLength: 32,
			},
			wantErr: true,
			errMsg:  "store ID is required",
		},
		{
			name: "zero token length",
			config: Config{
				Store:       registry.NewID("s", "t"),
				TokenLength: 0,
			},
			wantErr: true,
			errMsg:  "token length must be positive",
		},
		{
			name: "negative token length",
			config: Config{
				Store:       registry.NewID("s", "t"),
				TokenLength: -1,
			},
			wantErr: true,
			errMsg:  "token length must be positive",
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

func TestConfig_InitDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected Config
	}{
		{
			name: "zero values get defaults",
			config: Config{
				Store: registry.NewID("s", "t"),
			},
			expected: Config{
				Store:             registry.NewID("s", "t"),
				TokenLength:       32,
				DefaultExpiration: 24 * time.Hour,
			},
		},
		{
			name: "existing values preserved",
			config: Config{
				Store:             registry.NewID("s", "t"),
				TokenLength:       64,
				DefaultExpiration: 48 * time.Hour,
			},
			expected: Config{
				Store:             registry.NewID("s", "t"),
				TokenLength:       64,
				DefaultExpiration: 48 * time.Hour,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.InitDefaults()
			assert.Equal(t, tt.expected, tt.config)
		})
	}
}
