package process

import (
	"context"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
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
)

type (
	NodeID = string
	HostID = string

	PID struct {
		Node NodeID
		Host HostID
		ID   registry.ID
		Name string
	}

	// Prototype is a function that creates a new process instance.
	// It follows the prototype pattern, where each call creates a new instance
	// based on the prototype's template.
	Prototype func() (Process, error)

	// Factory manages process prototypes and handles process creation
	Factory interface {
		// Create instantiates a new process from the registered prototype.
		// Returns error if prototype not found or creation fails.
		Create(registry.ID) (Process, error)
	}

	Message struct {
		Topic   string
		Payload payload.Payloads
	}

	// Process represents a long-running workflow that can be controlled
	// through various layer interfaces. Stepping can be used to batch
	// internal state changes between external interactions.
	Process interface {
		// Start begins process execution with given task.
		Start(ctx context.Context, pid PID, input payload.Payloads) error

		// Step advances process state by one iteration
		Step() error

		// Send delivers a message to the process instance.
		Send(msg Message) error

		// OnComplete registers a callback for process completion/failure
		// Returns false if process already completed
		OnComplete(func(PID, runtime.Result)) bool
	}

	Launch struct {
		HostID   HostID
		ID       registry.ID
		Name     string
		Payloads payload.Payloads
	}

	Manager interface {
		// Launch creates and starts a new process instance on the specified host
		Launch(ctx context.Context, launch Launch) (PID, error)
	}

	// Host core interface for process control
	Host interface {
		Send(ctx context.Context, pid PID, msg payload.Payloads) error
		Terminate(ctx context.Context, pid PID) error
	}

	// Managed handles local process operations
	Managed interface {
		Host
		Launch(ctx context.Context, pid PID, prototype Process, input payload.Payloads) (PID, error)
	}

	// Delegated handles remote process operations
	Delegated interface {
		Host
		Launch(ctx context.Context, pid PID, input payload.Payloads) (PID, error)
	}
)

func GetProcesses(ctx context.Context) Manager {
	return ctx.Value(contextapi.ProcessesCtx).(Manager)
}
