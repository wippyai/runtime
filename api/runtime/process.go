package runtime

import (
	"context"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
)

// Event system and kind constants for the workflow package
const (
	// ProcessSystem identifies the workflow system in the event bus.
	ProcessSystem events.System = "processes"

	// RegisterPrototype is the event kind for registering a new process prototype.
	RegisterPrototype events.Kind = "processes.register"

	// DeletePrototype is the event kind for removing an existing process prototype.
	DeletePrototype events.Kind = "processes.remove"

	// AcceptPrototype is the event kind for accepting a new process prototype.
	AcceptPrototype events.Kind = "processes.accept"

	// RejectPrototype is the event kind for rejecting a new process prototype.
	RejectPrototype events.Kind = "processes.reject"
)

type (
	// ProcessPrototype is a function that creates a new process instance.
	// It follows the prototype pattern, where each call creates a new instance
	// based on the prototype's template.
	ProcessPrototype func() (Process, error)

	// ProcessFactory manages process prototypes and handles process creation
	ProcessFactory interface {
		// Create instantiates a new process from the registered prototype.
		// Returns error if prototype not found or creation fails.
		Create(registry.ID) (Process, error)
	}

	// Process represents a long-running workflow that can be controlled
	// through various layer interfaces. Stepping can be used to batch
	// internal state changes between external interactions.
	Process interface {
		// Start begins process execution with given task.
		Start(ctx context.Context, task Task) error

		// Done returns a channel that is closed when the process is complete or exited.
		Done() <-chan struct{}

		// Result returns the final result of the process execution. Only call after done.
		Result() *Result
	}
)

func GetProcessFactory(ctx context.Context) ProcessFactory {
	return ctx.Value(contextapi.ProcessesCtx).(ProcessFactory)
}
