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

	// AccessKeyID is the AWS access key ID.
	AccessKeyID string `json:"access_key_id,omitempty"`

	// SecretAccessKey is the AWS secret access key.
	SecretAccessKey string `json:"secret_access_key,omitempty"`
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.Region == "" {
		return errors.New("region is required")
	}

	return nil
}
