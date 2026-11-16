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
//
// Credentials are either static or taken from the AWS SDK default credential
// chain. An entry that sets none of access_key_id, secret_access_key,
// access_key_id_env and secret_access_key_env uses the default chain. An entry
// that sets any of them declares static credentials, and both values must
// resolve to non-empty strings at decode; the default chain is never used in
// their place.
type Config struct {
	// Region is the AWS region where the bucket is located.
	Region string `json:"region"`

	// AccessKeyID is the AWS access key ID.
	AccessKeyID string `json:"access_key_id,omitempty"`

	// SecretAccessKey is the AWS secret access key.
	SecretAccessKey string `json:"secret_access_key,omitempty"`

	// AccessKeyIDEnv names the environment variable AccessKeyID resolves from.
	AccessKeyIDEnv string `json:"access_key_id_env,omitempty"`

	// SecretAccessKeyEnv names the environment variable SecretAccessKey
	// resolves from.
	SecretAccessKeyEnv string `json:"secret_access_key_env,omitempty"`
}

// StaticCredentials reports whether the entry declares static credentials.
func (c *Config) StaticCredentials() bool {
	return c.AccessKeyID != "" || c.SecretAccessKey != "" ||
		c.AccessKeyIDEnv != "" || c.SecretAccessKeyEnv != ""
}

// Validate checks if the configuration is valid. Credential values are
// resolved before validation, so a declared credential that is still empty
// names an unset environment variable or a missing half of the pair.
func (c *Config) Validate() error {
	if c.Region == "" {
		return errors.New("region is required")
	}

	if !c.StaticCredentials() {
		return nil
	}
	if c.AccessKeyID == "" {
		return NewUnresolvedCredentialError("access_key_id", c.AccessKeyIDEnv)
	}
	if c.SecretAccessKey == "" {
		return NewUnresolvedCredentialError("secret_access_key", c.SecretAccessKeyEnv)
	}
	return nil
}
