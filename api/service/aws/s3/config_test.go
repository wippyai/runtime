// SPDX-License-Identifier: MPL-2.0

// Package s3 provides S3 service configuration.
package s3

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKindConstant(t *testing.T) {
	assert.Equal(t, "cloudstorage.s3", Kind)
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
				Bucket:    "my-bucket",
				AWSConfig: "aws-config-resource",
				Endpoint:  "https://s3.amazonaws.com",
			},
			wantErr: false,
		},
		{
			name: "minimal config",
			config: Config{
				Bucket:    "test-bucket",
				AWSConfig: "aws-cfg",
			},
			wantErr: false,
		},
		{
			name: "bucket and endpoint from env",
			config: Config{
				BucketEnv:   "S3_BUCKET",
				AWSConfig:   "aws-cfg",
				EndpointEnv: "S3_ENDPOINT",
			},
			wantErr: false,
		},
		{
			name: "with custom endpoint",
			config: Config{
				Bucket:    "local-bucket",
				AWSConfig: "aws-cfg",
				Endpoint:  "http://localhost:9000",
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
				Bucket:    "my-bucket",
				AWSConfig: "aws-cfg",
			},
			wantErr: false,
		},
		{
			name: "valid with endpoint",
			config: Config{
				Bucket:    "my-bucket",
				AWSConfig: "aws-cfg",
				Endpoint:  "http://minio:9000",
			},
			wantErr: false,
		},
		{
			name: "valid with bucket env",
			config: Config{
				BucketEnv: "S3_BUCKET",
				AWSConfig: "aws-cfg",
			},
			wantErr: false,
		},
		{
			name: "missing bucket",
			config: Config{
				AWSConfig: "aws-cfg",
			},
			wantErr: true,
			errMsg:  "bucket name is required",
		},
		{
			name: "empty bucket",
			config: Config{
				Bucket:    "",
				AWSConfig: "aws-cfg",
			},
			wantErr: true,
			errMsg:  "bucket name is required",
		},
		{
			name: "missing aws config",
			config: Config{
				Bucket: "my-bucket",
			},
			wantErr: true,
			errMsg:  "aws config is required",
		},
		{
			name: "empty aws config",
			config: Config{
				Bucket:    "my-bucket",
				AWSConfig: "",
			},
			wantErr: true,
			errMsg:  "aws config is required",
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
