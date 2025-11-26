package temporal

import (
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	tmcli "go.temporal.io/sdk/client"
)

// Temporal system event constants
const (
	// System identifies the temporal system in the event bus
	System event.System = "temporal"

	// SystemTemporalTaskQueue is the system for task queue events
	SystemTemporalTaskQueue event.System = "temporal.taskqueue"

	// TaskQueueRegister is the event kind for registering a task queue
	TaskQueueRegister event.Kind = "taskqueue.register"

	// WorkflowRegister is the event kind for registering a workflow
	WorkflowRegister event.Kind = "workflow.register"
)

// TaskQueueRegistration represents a request to register a task queue
type TaskQueueRegistration struct {
	ID        registry.ID
	Client    registry.ID
	TaskQueue string
}

// WorkflowRegistration represents a workflow registration
type WorkflowRegistration struct {
	Source    registry.ID
	TaskQueue registry.ID
	Name      string
	Options   *tmcli.StartWorkflowOptions
	Handler   any
}
