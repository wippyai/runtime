// Package s3 provides S3 service configuration.
package s3

import (
	"errors"

	"github.com/ponyruntime/pony/api/registry"
)

// Kind identifies the S3 storage service type.
const Kind registry.Kind = "cloudstorage.s3"

// Config represents configuration for an S3 storage provider.
type Config struct {
	// Bucket is the S3 bucket name.
	Bucket string `json:"bucket"`

	// AWSConfig is a resource name of aws config.
	AWSConfig string `json:"config"`

	// AccessKeyIDEnv is the AWS access key ID env name.
	AccessKeyIDEnv string `json:"access_key_id_env,omitempty"`

	// SecretAccessKeyEnv is the AWS secret access key env name.
	SecretAccessKeyEnv string `json:"secret_access_key_env,omitempty"`

	// Endpoint is a custom endpoint.
	Endpoint string `json:"endpoint"`
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.Bucket == "" {
		return errors.New("bucket name cannot be empty")
	}

	if c.AWSConfig == "" {
		return errors.New("aws config can't be empty")
	}

	return nil
}
