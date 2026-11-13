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
				Region:          "us-west-2",
				AccessKeyID:     "AKIAEXAMPLE",
				SecretAccessKey: "secret",
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
			name: "with credentials",
			config: Config{
				Region:          "ap-southeast-1",
				AccessKeyID:     "KEY_ID",
				SecretAccessKey: "SECRET_KEY",
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
			name: "valid with credentials",
			config: Config{
				Region:          "us-west-1",
				AccessKeyID:     "AWS_KEY",
				SecretAccessKey: "AWS_SECRET",
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
		{
			name: "valid with resolved env directives",
			config: Config{
				Region:             "us-east-1",
				AccessKeyID:        "AWS_KEY",
				SecretAccessKey:    "AWS_SECRET",
				AccessKeyIDEnv:     "KEY_VAR",
				SecretAccessKeyEnv: "SECRET_VAR",
			},
			wantErr: false,
		},
		{
			name: "access key directive unresolved",
			config: Config{
				Region:             "us-east-1",
				AccessKeyIDEnv:     "KEY_VAR",
				SecretAccessKeyEnv: "SECRET_VAR",
			},
			wantErr: true,
			errMsg:  "access_key_id_env names environment variable KEY_VAR, which is not set",
		},
		{
			name: "secret directive unresolved",
			config: Config{
				Region:             "us-east-1",
				AccessKeyID:        "AWS_KEY",
				SecretAccessKeyEnv: "SECRET_VAR",
			},
			wantErr: true,
			errMsg:  "secret_access_key_env names environment variable SECRET_VAR, which is not set",
		},
		{
			name: "secret missing",
			config: Config{
				Region:      "us-east-1",
				AccessKeyID: "AWS_KEY",
			},
			wantErr: true,
			errMsg:  "secret_access_key is required",
		},
		{
			name: "access key missing",
			config: Config{
				Region:          "us-east-1",
				SecretAccessKey: "AWS_SECRET",
			},
			wantErr: true,
			errMsg:  "access_key_id is required",
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
