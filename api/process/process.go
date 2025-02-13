package process

import (
	"context"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
)

// Event system and kind constants for the workflow package
const (
	// PrototypeSystem identifies the workflow system in the event bus.
	PrototypeSystem events.System = "prototypes"

	// RegisterPrototype is the event kind for registering a new process prototype.
	RegisterPrototype events.Kind = "prototypes.register"

	// DeletePrototype is the event kind for removing an existing process prototype.
	DeletePrototype events.Kind = "prototypes.remove"

	// AcceptPrototype is the event kind for accepting a new process prototype.
	AcceptPrototype events.Kind = "prototypes.accept"

	// RejectPrototype is the event kind for rejecting a new process prototype.
	RejectPrototype events.Kind = "prototypes.reject"
)

type (

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

	// Process represents a long-running workflow that can be controlled
	// through various layer interfaces. Stepping can be used to batch
	// internal state changes between external interactions.
	Process interface {
		// Start begins process execution with given task.
		Start(ctx context.Context, task runtime.Task) error

		// Step advances process state by one iteration
		Step() error

		// Done returns a channel that is closed when the process is complete or exited.
		Done() <-chan struct{}

		// Result returns the final result of the process execution. Only call after done.
		Result() *runtime.Result
	}
)

func GetProcessFactory(ctx context.Context) Factory {
	return ctx.Value(contextapi.ProcessFactoryCtx).(Factory)
}
