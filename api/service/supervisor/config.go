// Package supervisor provides service lifecycle management and supervision.
package supervisor

import (
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/api/topology"
)

const KindProcessService = "process.service"

// ServiceConfig provides configuration for a process service with lifecycle management.
type ServiceConfig struct {
	// Process that will be used to start the process
	Process registry.ID `json:"process" yaml:"process"`

	// Host Process where the process should be started
	HostID pid.HostID `json:"host" yaml:"host"`

	// Input to be passed to the process as input
	Input []any `json:"input" yaml:"input"`

	// Lifecycle configuration for supervisor
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle" yaml:"lifecycle"`
}

// Validate checks if the configuration is valid
func (c *ServiceConfig) Validate() error {
	if c.Process.Name == "" {
		return ErrProcessRequired
	}

	if c.HostID == "" {
		return ErrHostRequired
	}

	if c.HostID == topology.ControlHost {
		return NewInvalidHostError(c.HostID)
	}

	return nil
}
