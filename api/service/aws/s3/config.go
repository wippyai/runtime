// SPDX-License-Identifier: MPL-2.0

// Package s3 provides S3 service configuration.
package s3

import (
	"errors"

	"github.com/wippyai/runtime/api/registry"
)

// Kind identifies the S3 storage service type.
const Kind registry.Kind = "cloudstorage.s3"

// Config represents configuration for an S3 storage provider.
type Config struct {
	// Bucket is the S3 bucket name.
	Bucket string `json:"bucket"`

	// BucketEnv is an env registry variable holding the S3 bucket name.
	BucketEnv string `json:"bucket_env,omitempty"`

	// AWSConfig is a resource name of aws config.
	AWSConfig string `json:"config"`

	// Endpoint is the custom S3 endpoint URL (optional, for S3-compatible services).
	Endpoint string `json:"endpoint"`

	// EndpointEnv is an env registry variable holding the custom S3 endpoint URL.
	EndpointEnv string `json:"endpoint_env,omitempty"`
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.Bucket == "" && c.BucketEnv == "" {
		return errors.New("bucket name is required")
	}

	if c.AWSConfig == "" {
		return errors.New("aws config is required")
	}

	return nil
}
