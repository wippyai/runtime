// Package s3 provides S3 service configuration.
package s3

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKindConstant(t *testing.T) {
	assert.Equal(t, "cloudstorage.s3", string(Kind))
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
				Bucket:             "my-bucket",
				AWSConfig:          "aws-config-resource",
				AccessKeyIDEnv:     "AWS_ACCESS_KEY_ID",
				SecretAccessKeyEnv: "AWS_SECRET_ACCESS_KEY",
				Endpoint:           "https://s3.amazonaws.com",
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
		name    string
		config  Config
		wantErr bool
		errMsg  string
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
			name: "missing bucket",
			config: Config{
				AWSConfig: "aws-cfg",
			},
			wantErr: true,
			errMsg:  "bucket name cannot be empty",
		},
		{
			name: "empty bucket",
			config: Config{
				Bucket:    "",
				AWSConfig: "aws-cfg",
			},
			wantErr: true,
			errMsg:  "bucket name cannot be empty",
		},
		{
			name: "missing aws config",
			config: Config{
				Bucket: "my-bucket",
			},
			wantErr: true,
			errMsg:  "aws config can't be empty",
		},
		{
			name: "empty aws config",
			config: Config{
				Bucket:    "my-bucket",
				AWSConfig: "",
			},
			wantErr: true,
			errMsg:  "aws config can't be empty",
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
