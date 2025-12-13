// Package terminal provides terminal service configuration.
package terminal

import (
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// KindHost identifies a terminal service component
const KindHost registry.Kind = "terminal.host"

type (
	// HostConfig represents the configuration for a terminal service
	HostConfig struct {
		HideLogs  bool                       `json:"hide_logs"` // Redirect logs (all) to the event bus, releases io.Output
		Lifecycle supervisor.LifecycleConfig `json:"lifecycle"` // Lifecycle management config
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
