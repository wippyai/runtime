package process

import (
	"context"
	"errors"
	"fmt"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/api/topology"
)

// Event system and kind constants for the workflow package
const (
	// Auto-supervision
	KindProcessService = "process.service"

	// PrototypeSystem identifies the workflow system in the event bus.
	PrototypeSystem events.System = "prototype"

	// RegisterPrototype is the event kind for registering a new process prototype.
	RegisterPrototype events.Kind = "prototype.register"

	// DeletePrototype is the event kind for removing an existing process prototype.
	DeletePrototype events.Kind = "prototype.remove"

	// AcceptPrototype is the event kind for accepting a new process prototype.
	AcceptPrototype events.Kind = "prototype.accept"

	// RejectPrototype is the event kind for rejecting a new process prototype.
	RejectPrototype events.Kind = "prototype.reject"

	HostSystem   events.System = "hosts"
	RegisterHost events.Kind   = "hosts.register"
	DeleteHost   events.Kind   = "hosts.remove"
	AcceptHost   events.Kind   = "hosts.accept"
	RejectHost   events.Kind   = "hosts.reject"

	TopicEvents = topology.TopicEvents
)

var (
	ErrNoProcess  = errors.New("no process running")
	ErrHostBusy   = errors.New("process host is busy")
	ErrTerminated = errors.New("process terminated")
)

type (
	ServiceConfig struct {
		// Process ID that will be used to start the process
		ID registry.ID `json:"id" yaml:"id"`

		// Host ID where the process should be started
		HostID pubsub.HostID `json:"host" yaml:"host"`

		// todo: payload ?

		// Lifecycle configuration for supervisor
		Lifecycle supervisor.LifecycleConfig `json:"lifecycle" yaml:"lifecycle"`
	}

	// Prototype is a function that creates a new process instance.
	Prototype func() (Process, error)

	// Factory manages process prototypes and handles process creation
	Factory interface {
		Create(registry.ID) (Process, error)
	}

	Process interface {
		pubsub.Downstream

		Start(context.Context, pubsub.PID, payload.Payloads) error

		// Step advances process state by one iteration
		Step() error
	}

	StartProcess struct {
		HostID   pubsub.HostID
		ID       registry.ID
		UniqID   string
		Payloads payload.Payloads
	}

	Manager interface {
		Start(ctx context.Context, start *StartProcess) (pubsub.PID, error)
		StartMonitored(context.Context, pubsub.PID, *StartProcess) (pubsub.PID, error)
		Send(ctx context.Context, pid pubsub.PID, msg *pubsub.Batch) error
		Terminate(ctx context.Context, pid pubsub.PID) error
	}

	Host interface {
		Send(ctx context.Context, pid pubsub.PID, msg *pubsub.Batch) error
		Terminate(ctx context.Context, pid pubsub.PID) error
	}

	LaunchProcess struct {
		PID     pubsub.PID
		Process Process
		Input   payload.Payloads
	}

	Managed interface {
		Host
		Launch(ctx context.Context, launch *LaunchProcess) (pubsub.PID, error)
	}

	Delegated interface {
		Host
		Launch(ctx context.Context, pid pubsub.PID, input payload.Payloads) (pubsub.PID, error)
	}
)

func GetProcesses(ctx context.Context) Manager {
	return ctx.Value(contextapi.ProcessesCtx).(Manager)
}

// Validate checks if the configuration is valid
func (c *ServiceConfig) Validate() error {
	if c.ID.Name == "" {
		return fmt.Errorf("process ID is required")
	}

	if c.HostID == "" {
		return fmt.Errorf("host ID is required")
	}

	if c.HostID == topology.ControlHost {
		return fmt.Errorf("host ID cannot be %s", topology.ControlHost)
	}

	return nil
}
