// Package supervisor provides service lifecycle management and supervision.
package supervisor

import (
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/api/topology"
)

// ProcessService identifies process service entries in the registry.
const ProcessService = "process.service"

// ServiceConfig provides configuration for a process service with lifecycle management.
type ServiceConfig struct {
	Process   registry.ID                `json:"process" yaml:"process"`
	HostID    pid.HostID                 `json:"host" yaml:"host"`
	Input     []any                      `json:"input" yaml:"input"`
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
