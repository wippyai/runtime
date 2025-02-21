package supervisor

import (
	"fmt"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/api/topology"
)

const KindProcessService = "process.service"

type ServiceConfig struct {
	// Process that will be used to start the process
	Process registry.ID `json:"process" yaml:"process"`

	// Host Process where the process should be started
	HostID pubsub.HostID `json:"host" yaml:"host"`

	// Payloads to be passed to the process as input
	Input []any `json:"input" yaml:"input"`

	// Lifecycle configuration for supervisor
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle" yaml:"lifecycle"`
}

// Validate checks if the configuration is valid
func (c *ServiceConfig) Validate() error {
	if c.Process.Name == "" {
		return fmt.Errorf("process Process is required")
	}

	if c.HostID == "" {
		return fmt.Errorf("host Process is required")
	}

	if c.HostID == topology.ControlHost {
		return fmt.Errorf("host Process cannot be %s", topology.ControlHost)
	}

	return nil
}
