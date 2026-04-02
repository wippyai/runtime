// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Kind identifies the AWS SQS queue driver.
const Kind registry.Kind = "queue.driver.sqs"

// Config defines the AWS SQS queue driver configuration.
type Config struct {
	Region    string                     `json:"region"`
	Endpoint  string                     `json:"endpoint"`
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	return nil
}

// InitDefaults initializes default values.
func (c *Config) InitDefaults() {
}
