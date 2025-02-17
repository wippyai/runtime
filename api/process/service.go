package process

import (
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
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
