package process

import (
	"context"
	"errors"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
)

// Event system and kind constants for the workflow package
const (
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

	TopicCancel pubsub.Topic = "@process/cancel"
	TopicEvents pubsub.Topic = "@process/events"
)

var (
	ErrNoProcess  = errors.New("no process running")
	ErrHostBusy   = errors.New("process host is busy")
	ErrTerminated = errors.New("process terminated")
)

type (
	// Prototype is a function that creates a new process instance.
	Prototype func() (Process, error)

	// Factory manages process prototypes and handles process creation
	Factory interface {
		Create(registry.ID) (Process, error)
	}

	Process interface {
		pubsub.Receiver

		Start(context.Context, pubsub.PID, payload.Payloads) error

		// Step advances process state by one iteration
		Step() error
	}

	StartProcess struct {
		HostID   pubsub.HostID
		ID       registry.ID
		Name     string
		Payloads payload.Payloads
	}

	Manager interface {
		Start(ctx context.Context, start *StartProcess) (pubsub.PID, error)
		Send(ctx context.Context, pid pubsub.PID, msg ...*pubsub.Message) error
		Terminate(ctx context.Context, pid pubsub.PID) error
	}

	Host interface {
		Send(ctx context.Context, pid pubsub.PID, msg ...*pubsub.Message) error
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
