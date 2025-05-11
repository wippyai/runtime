package temporal

import (
	"fmt"
	"time"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// Registry kind constants for Temporal service components
const (
	// KindClient identifies a temporal client component
	KindClient registry.Kind = "temporal.client"

	// KindTaskQueue identifies a temporal task queue component
	KindTaskQueue registry.Kind = "temporal.task_queue"

	// KindWorkflow identifies a temporal workflow definition
	KindWorkflow registry.Kind = "temporal.workflow"

	// KindActivity identifies a temporal activity function
	KindActivity registry.Kind = "temporal.activity"
)

// Temporal system event constants
const (
	// System identifies the temporal system in the event bus
	System event.System = "temporal"

	// TaskQueueRegister is the event kind for registering a task queue
	TaskQueueRegister event.Kind = "taskqueue.register"

	// TaskQueueUpdate is the event kind for updating a task queue
	TaskQueueUpdate event.Kind = "taskqueue.update"

	// TaskQueueDelete is the event kind for deleting a task queue
	TaskQueueDelete event.Kind = "taskqueue.delete"

	// TaskQueueAccept is the event kind sent when a task queue registration is accepted
	TaskQueueAccept event.Kind = "taskqueue.accept"

	// TaskQueueReject is the event kind sent when a task queue registration is rejected
	TaskQueueReject event.Kind = "taskqueue.reject"

	// WorkflowRegister is the event kind for registering a workflow
	WorkflowRegister event.Kind = "workflow.register"

	// WorkflowUpdate is the event kind for updating a workflow
	WorkflowUpdate event.Kind = "workflow.update"

	// WorkflowDelete is the event kind for deleting a workflow
	WorkflowDelete event.Kind = "workflow.delete"

	// WorkflowAccept is the event kind sent when a workflow registration is accepted
	WorkflowAccept event.Kind = "workflow.accept"

	// WorkflowReject is the event kind sent when a workflow registration is rejected
	WorkflowReject event.Kind = "workflow.reject"

	// ActivityRegister is the event kind for registering an activity
	ActivityRegister event.Kind = "activity.register"

	// ActivityUpdate is the event kind for updating an activity
	ActivityUpdate event.Kind = "activity.update"

	// ActivityDelete is the event kind for deleting an activity
	ActivityDelete event.Kind = "activity.delete"

	// ActivityAccept is the event kind sent when an activity registration is accepted
	ActivityAccept event.Kind = "activity.accept"

	// ActivityReject is the event kind sent when an activity registration is rejected
	ActivityReject event.Kind = "activity.reject"
)

// TaskQueueRegistration represents a request to register a task queue
type TaskQueueRegistration struct {
	ID        registry.ID                // ID of the task queue
	Client    registry.ID                // Reference to client ID
	TaskQueue string                     // Task queue name
	Options   worker.Options             // Official Temporal worker options
	Lifecycle supervisor.LifecycleConfig // Lifecycle management config
}

// InitDefaults initializes the TaskQueueRegistration with default values
func (c *TaskQueueRegistration) InitDefaults() {
	// Initialize with standard defaults from Temporal SDK
	if c.Options.MaxConcurrentActivityExecutionSize <= 0 {
		c.Options.MaxConcurrentActivityExecutionSize = 1000
	}
	if c.Options.MaxConcurrentWorkflowTaskExecutionSize <= 0 {
		c.Options.MaxConcurrentWorkflowTaskExecutionSize = 1000
	}
	if c.Options.MaxConcurrentLocalActivityExecutionSize <= 0 {
		c.Options.MaxConcurrentLocalActivityExecutionSize = 1000
	}
	if c.Options.StickyScheduleToStartTimeout <= 0 {
		c.Options.StickyScheduleToStartTimeout = 5 * time.Second
	}
	if c.Options.MaxConcurrentActivityTaskPollers <= 0 {
		c.Options.MaxConcurrentActivityTaskPollers = 2
	}
	if c.Options.MaxConcurrentWorkflowTaskPollers <= 0 {
		c.Options.MaxConcurrentWorkflowTaskPollers = 2
	}
	if c.Options.MaxConcurrentSessionExecutionSize <= 0 {
		c.Options.MaxConcurrentSessionExecutionSize = 1000
	}

	// Initialize lifecycle defaults
	c.Lifecycle.InitDefaults()
}

// Validate checks if the task queue configuration is valid
func (c *TaskQueueRegistration) Validate() error {
	// Client reference is required
	if c.Client.String() == "" {
		return fmt.Errorf("client reference cannot be empty")
	}

	// Task queue name is required
	if c.TaskQueue == "" {
		return fmt.Errorf("task queue name cannot be empty")
	}

	// Validate concurrency settings
	if c.Options.MaxConcurrentActivityExecutionSize <= 0 {
		return fmt.Errorf("max concurrent activity execution must be positive")
	}
	if c.Options.MaxConcurrentWorkflowTaskExecutionSize <= 0 {
		return fmt.Errorf("max concurrent workflow execution must be positive")
	}
	if c.Options.MaxConcurrentWorkflowTaskPollers == 1 {
		return fmt.Errorf("max concurrent workflow task pollers cannot be 1 due to sticky/non-sticky queue logic")
	}

	// Validate rate limits if set
	if c.Options.WorkerActivitiesPerSecond < 0 {
		return fmt.Errorf("worker activities per second cannot be negative")
	}
	if c.Options.WorkerLocalActivitiesPerSecond < 0 {
		return fmt.Errorf("worker local activities per second cannot be negative")
	}
	if c.Options.TaskQueueActivitiesPerSecond < 0 {
		return fmt.Errorf("task queue activities per second cannot be negative")
	}

	// Cannot disable both workflow and activity workers
	if c.Options.DisableWorkflowWorker && c.Options.LocalActivityWorkerOnly {
		return fmt.Errorf("cannot set both DisableWorkflowWorker and LocalActivityWorkerOnly")
	}

	return nil
}

// GetLifecycleConfig returns the lifecycle configuration with client dependency
func (c *TaskQueueRegistration) GetLifecycleConfig() supervisor.LifecycleConfig {
	cfg := c.Lifecycle

	// Ensure dependencies includes the client
	if cfg.DependsOn == nil {
		cfg.DependsOn = []string{c.Client.String()}
	} else {
		// Check if client is already in dependencies
		found := false
		for _, dep := range cfg.DependsOn {
			if dep == c.Client.String() {
				found = true
				break
			}
		}

		// Add client to dependencies if not already included
		if !found {
			cfg.DependsOn = append(cfg.DependsOn, c.Client.String())
		}
	}

	return cfg
}

// WorkflowRegistration represents a request to register a workflow
type WorkflowRegistration struct {
	TaskQueue     registry.ID                  // id of the task queue to use
	Name          string                       // Name for the workflow registration
	Options       *client.StartWorkflowOptions // Default options for workflow execution
	WakeUpSignals []string                     // Signals to automatically start workflow on
	Handler       any                          // Optional handler for the workflow
}

// ActivityRegistration represents a request to register an activity
type ActivityRegistration struct {
	TaskQueue registry.ID // id of the task queue to use
	Name      string      // Name for the activity registration
	Handler   any         // Optional handler for the activity
}

// TaskQueueDeletion represents a request to delete a task queue
type TaskQueueDeletion struct {
	TaskQueue registry.ID // Task queue id to delete
}

// WorkflowDeletion represents a request to delete a workflow
type WorkflowDeletion struct {
	TaskQueue    registry.ID // Task queue id where the workflow is registered
	WorkflowName string      // Name of the workflow to delete
}

// ActivityDeletion represents a request to delete an activity
type ActivityDeletion struct {
	TaskQueue    registry.ID // Task queue id where the activity is registered
	ActivityName string      // Name of the activity to delete
}
