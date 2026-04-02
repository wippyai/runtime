// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"errors"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Kind identifies the AMQP (RabbitMQ) queue driver.
const Kind registry.Kind = "queue.driver.amqp"

// Config defines the AMQP queue driver configuration.
type Config struct {
	URL       string                     `json:"url"`
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.URL == "" {
		return errors.New("amqp url is required")
	}
	return nil
}

// InitDefaults initializes default values.
func (c *Config) InitDefaults() {
	if c.URL == "" {
		c.URL = "amqp://guest:guest@localhost:5672/"
	}
}
