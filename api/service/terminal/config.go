// SPDX-License-Identifier: MPL-2.0

// Package terminal provides terminal service configuration.
package terminal

import (
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Host identifies a terminal service component
const Host registry.Kind = "terminal.host"

type (
	// HostConfig represents the configuration for a terminal service
	HostConfig struct {
		Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
		HideLogs  bool                       `json:"hide_logs"`
	}
)

// initDefaults initializes the HostConfig with default values
func (c *HostConfig) initDefaults() {
	c.Lifecycle.InitDefaults()
}

func (c *HostConfig) Validate() error {
	c.initDefaults()
	return nil
}
