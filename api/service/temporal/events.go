package temporal

import (
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	tmcli "go.temporal.io/sdk/client"
)

// Temporal system event constants
const (
	// SystemTemporalTaskQueue is the system for task queue events
	SystemTemporalTaskQueue event.System = "temporal.taskqueue"

	// TaskQueueRegister is the event kind for registering a task queue
	TaskQueueRegister event.Kind = "taskqueue.register"
)

// TaskQueueRegistration represents a request to register a task queue.
type TaskQueueRegistration struct {
	// ID is the worker registry ID that owns this task queue.
	ID registry.ID
	// Client is the Temporal client the worker connects through.
	Client registry.ID
	// TaskQueue is the Temporal task queue name.
	TaskQueue string
}

// WorkflowRegistration represents a workflow registration.
type WorkflowRegistration struct {
	// Handler is the workflow definition factory.
	Handler any
	// Options provides default start options for the workflow.
	Options *tmcli.StartWorkflowOptions
	// Source is the registry ID of the workflow entry.
	Source registry.ID
	// TaskQueue is the worker registry ID where the workflow is registered.
	TaskQueue registry.ID
	// Name is the Temporal workflow type name.
	Name string
}
