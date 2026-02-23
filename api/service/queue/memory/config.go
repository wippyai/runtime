// SPDX-License-Identifier: MPL-2.0

package memory

import (
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Kind identifies the in-memory queue driver.
const Kind registry.Kind = "queue.driver.memory"

// Config defines the in-memory queue driver configuration.
type Config struct {
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	return nil
}

// InitDefaults initializes default values.
func (c *Config) InitDefaults() {
}
