// SPDX-License-Identifier: MPL-2.0

package redis

import (
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Kind identifies the Redis Streams queue driver.
const Kind registry.Kind = "queue.driver.redis"

// Config defines the Redis Streams queue driver configuration.
type Config struct {
	Addr      string                     `json:"addr"`
	Password  string                     `json:"password"`
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
	DB        int                        `json:"db"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	return nil
}

// InitDefaults initializes default values.
func (c *Config) InitDefaults() {
	if c.Addr == "" {
		c.Addr = "localhost:6379"
	}
}
