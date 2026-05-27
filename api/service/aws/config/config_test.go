// SPDX-License-Identifier: MPL-2.0

// Package config provides AWS service configuration.
package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKindConstant(t *testing.T) {
	assert.Equal(t, "config.aws", Kind)
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
				Region:             "us-west-2",
				AccessKeyIDEnv:     "AWS_ACCESS_KEY_ID",
				SecretAccessKeyEnv: "AWS_SECRET_ACCESS_KEY",
			},
			wantErr: false,
		},
		{
			name: "minimal config",
			config: Config{
				Region: "eu-central-1",
			},
			wantErr: false,
		},
		{
			name: "region from env",
			config: Config{
				RegionEnv: "AWS_REGION",
			},
			wantErr: false,
		},
		{
			name: "with env vars only",
			config: Config{
				Region:             "ap-southeast-1",
				AccessKeyIDEnv:     "KEY_ID",
				SecretAccessKeyEnv: "SECRET_KEY",
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
			assert.Equal(t, tt.config, decoded)
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		config  Config
		name    string
		errMsg  string
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Region: "us-east-1",
			},
			wantErr: false,
		},
		{
			name: "valid with env vars",
			config: Config{
				Region:             "us-west-1",
				AccessKeyIDEnv:     "AWS_KEY",
				SecretAccessKeyEnv: "AWS_SECRET",
			},
			wantErr: false,
		},
		{
			name: "valid with region env",
			config: Config{
				RegionEnv: "AWS_REGION",
			},
			wantErr: false,
		},
		{
			name:    "missing region",
			config:  Config{},
			wantErr: true,
			errMsg:  "region is required",
		},
		{
			name: "empty region",
			config: Config{
				Region: "",
			},
			wantErr: true,
			errMsg:  "region is required",
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
