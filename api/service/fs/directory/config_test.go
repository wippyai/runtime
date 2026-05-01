// SPDX-License-Identifier: MPL-2.0

// Package directory provides directory service configuration.
package directory

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKindConstant(t *testing.T) {
	assert.Equal(t, "fs.directory", Kind)
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
				Directory: "/var/data",
				AutoInit:  true,
				Mode:      "0755",
				Type:      "",
				Base:      BaseProject,
			},
			wantErr: false,
		},
		{
			name: "module-relative config",
			config: Config{
				Directory: "./static/app",
				Mode:      "0755",
				Base:      BaseModule,
			},
			wantErr: false,
		},
		{
			name: "minimal config",
			config: Config{
				Directory: "/tmp",
			},
			wantErr: false,
		},
		{
			name: "read-only config",
			config: Config{
				Directory: "/usr/share",
				Mode:      "0444",
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
			assert.Equal(t, tt.config.Directory, decoded.Directory)
			assert.Equal(t, tt.config.AutoInit, decoded.AutoInit)
			assert.Equal(t, tt.config.Mode, decoded.Mode)
			assert.Equal(t, tt.config.Type, decoded.Type)
			assert.Equal(t, tt.config.Base, decoded.Base)
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		errMsg  string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Directory: "/var/data",
				Mode:      "0755",
			},
			wantErr: false,
		},
		{
			name: "empty directory",
			config: Config{
				Directory: "",
			},
			wantErr: true,
			errMsg:  "directory path is required",
		},
		{
			name: "valid mode 0444",
			config: Config{
				Directory: "/data",
				Mode:      "0444",
			},
			wantErr: false,
		},
		{
			name: "valid mode 0700",
			config: Config{
				Directory: "/private",
				Mode:      "0700",
			},
			wantErr: false,
		},
		{
			name: "invalid mode format",
			config: Config{
				Directory: "/data",
				Mode:      "invalid",
			},
			wantErr: true,
			errMsg:  "invalid file mode format",
		},
		{
			name: "no mode specified defaults to 0755",
			config: Config{
				Directory: "/data",
			},
			wantErr: false,
		},
		{
			name: "valid module base",
			config: Config{
				Directory: "./static/app",
				Base:      BaseModule,
			},
			wantErr: false,
		},
		{
			name: "valid project base",
			config: Config{
				Directory: "./static/app",
				Base:      BaseProject,
			},
			wantErr: false,
		},
		{
			name: "invalid base",
			config: Config{
				Directory: "./static/app",
				Base:      "workspace",
			},
			wantErr: true,
			errMsg:  "invalid directory base",
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

func TestConfig_FileMode(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected uint32
	}{
		{
			name: "default mode",
			config: Config{
				Directory: "/data",
			},
			expected: 0755,
		},
		{
			name: "mode 0444 after validate",
			config: Config{
				Directory: "/data",
				Mode:      "0444",
			},
			expected: 0444,
		},
		{
			name: "mode 0700 after validate",
			config: Config{
				Directory: "/data",
				Mode:      "0700",
			},
			expected: 0700,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, uint32(tt.config.GetMode()))
		})
	}
}
