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

	// RegisterSpawnCommand is the event kind for registering a new workflow handler.
	RegisterSpawnCommand events.Kind = "processes.set_spawn"

	// DeleteSpawnCommand is the event kind for removing an existing workflow handler.
	DeleteSpawnCommand events.Kind = "processes.remove_spawn"

	// AcceptSpawn is the event kind for accepting a new workflow handler.
	AcceptSpawn events.Kind = "processes.accept_spawn"

	// RejectSpawn is the event kind for rejecting a new workflow handler.
	RejectSpawn events.Kind = "processes.reject_spawn"
)

type (
	// LayerName identifies a specific interface that a process provides
	LayerName string

	// Flavor represents a specific variant of process implementation
	Flavor string

	// RegisterSpawn represents a request to register a new process spawner
	RegisterSpawn struct {
		// Global ID of the process.
		ID registry.ID
		// Spawn creates process of given flavor or returns error if flavor not supported
		Spawn func(Flavor) (Process, error)
	}

	// DeleteSpawn represents a request to remove a process spawner
	DeleteSpawn struct {
		Target registry.ID
	}

	// ProcessRegistry manages process spawners and handles process creation
	ProcessRegistry interface {
		// Create creates new process instance with specified flavor
		Create(registry.ID, Flavor) (Process, error)
	}

	// Process represents a long-running workflow that can be controlled
	// through various layer interfaces, stepping can be used to batch
	// internal state changes between external interactions
	Process interface {
		// Start begins process execution with given task.
		Start(task Task) (chan *Result, error)

		// GetLayer retrieves specific interface layer from process
		// Layer type is determined by process's Flavor and capabilities
		GetLayer(LayerName) any

		// Step advances process state by one iteration
		Step() error

		// Done returns a channel that is closed when the process is complete or exited.
		Done() <-chan struct{}

		// Result returns the final result of the process execution.
		Result() *Result
	}
)

func GetProcesses(ctx context.Context) ProcessRegistry {
	return ctx.Value(contextapi.ProcessesCtx).(ProcessRegistry)
}
