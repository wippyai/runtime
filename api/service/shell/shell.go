package shell

import (
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
)

// KindHost identifies a terminal service component
const KindHost registry.Kind = "shell.host"

type (
	// HostConfig represents the configuration for a terminal service
	HostConfig struct {
		HideLogs  bool                       `json:"hide_logs"` // Redirect logs (all) to the event bus, releases io.Output
		Lifecycle supervisor.LifecycleConfig `json:"lifecycle"` // Lifecycle management config
	}
)

// InitDefaults initializes the HostConfig with default values
func (c *HostConfig) InitDefaults() {
	c.Lifecycle.InitDefaults()
}

func (c *HostConfig) Validate() error {
	return nil
}
