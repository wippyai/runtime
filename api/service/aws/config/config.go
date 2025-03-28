package config

import (
	"errors"
	"github.com/ponyruntime/pony/api/registry"
)

// Kind identifies the aws config service type.
const Kind registry.Kind = "config.aws"

// Config represents configuration for an S3 storage provider.
type Config struct {
	// Region is the AWS region where the bucket is located.
	Region string `json:"region"`

	// AccessKeyIDEnv is the AWS access key ID env name.
	AccessKeyIDEnv string `json:"access_key_id_env,omitempty"`

	// SecretAccessKeyEnv is the AWS secret access key env name.
	SecretAccessKeyEnv string `json:"secret_access_key_env,omitempty"`
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.Region == "" {
		return errors.New("either region or endpoint must be specified")
	}

	return nil
}
