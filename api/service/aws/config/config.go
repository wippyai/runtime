// SPDX-License-Identifier: MPL-2.0

// Package config provides AWS service configuration.
package config

import (
	"errors"

	"github.com/wippyai/runtime/api/registry"
)

// Kind identifies the aws config service type.
const Kind registry.Kind = "config.aws"

// Config represents configuration for AWS services.
type Config struct {
	// Region is the AWS region where the bucket is located.
	Region string `json:"region"`

	// RegionEnv is an env registry variable holding the AWS region.
	RegionEnv string `json:"region_env,omitempty"`

	// AccessKeyIDEnv is the AWS access key ID env name.
	AccessKeyIDEnv string `json:"access_key_id_env,omitempty"`

	// SecretAccessKeyEnv is the AWS secret access key env name.
	SecretAccessKeyEnv string `json:"secret_access_key_env,omitempty"`
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.Region == "" && c.RegionEnv == "" {
		return errors.New("region is required")
	}

	return nil
}
