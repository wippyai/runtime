package temporal

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.temporal.io/sdk/worker"
)

// Registry kind constants for Temporal service components
const (
	// KindTaskQueue identifies a temporal task queue component
	KindTaskQueue registry.Kind = "temporal.task_queue"
)

// TaskQueueConfig represents configuration for a Temporal task queue worker
type TaskQueueConfig struct {
	Meta                                registry.Metadata          `json:"meta"`                                              // Metadata
	Client                              registry.ID                `json:"client"`                                            // Reference to client ID
	TaskQueue                           string                     `json:"task_queue"`                                        // Task queue name
	MaxConcurrentActivityExecution      int                        `json:"max_concurrent_activity_execution"`                 // Max concurrent activity executions
	MaxConcurrentWorkflowExecution      int                        `json:"max_concurrent_workflow_execution"`                 // Max concurrent workflow executions
	MaxConcurrentLocalActivityExecution int                        `json:"max_concurrent_local_activity_execution,omitempty"` // Max concurrent local activity executions
	WorkerActivitiesPerSecond           float64                    `json:"worker_activities_per_second,omitempty"`            // Rate limiting for activities per worker
	WorkerLocalActivitiesPerSecond      float64                    `json:"worker_local_activities_per_second,omitempty"`      // Rate limiting for local activities per worker
	TaskQueueActivitiesPerSecond        float64                    `json:"task_queue_activities_per_second,omitempty"`        // Rate limiting for activities per task queue
	MaxConcurrentActivityPollers        int                        `json:"max_concurrent_activity_pollers,omitempty"`         // Max concurrent activity task pollers
	MaxConcurrentWorkflowPollers        int                        `json:"max_concurrent_workflow_pollers,omitempty"`         // Max concurrent workflow task pollers
	StickyScheduleToStartTimeout        string                     `json:"sticky_schedule_to_start_timeout,omitempty"`        // Sticky queue timeout
	EnableLoggingInReplay               bool                       `json:"enable_logging_in_replay,omitempty"`                // Enable logging in replay mode
	DisableWorkflowWorker               bool                       `json:"disable_workflow_worker,omitempty"`                 // Disable workflow worker
	LocalActivityWorkerOnly             bool                       `json:"local_activity_worker_only,omitempty"`              // Only process workflow tasks and local activities
	EnableSessionWorker                 bool                       `json:"enable_session_worker,omitempty"`                   // Enable session worker
	MaxConcurrentSessionExecutionSize   int                        `json:"max_concurrent_session_execution_size,omitempty"`   // Max concurrent session execution size
	WorkerStopTimeout                   string                     `json:"worker_stop_timeout,omitempty"`                     // Worker stop timeout
	DisableEagerActivities              bool                       `json:"disable_eager_activities,omitempty"`                // Disable eager activity execution
	MaxConcurrentEagerActivityExecution int                        `json:"max_concurrent_eager_activity_execution,omitempty"` // Max concurrent eager activity executions
	Lifecycle                           supervisor.LifecycleConfig `json:"lifecycle"`                                         // Lifecycle management config
}

// UnmarshalJSON implements custom unmarshaling for TaskQueueConfig to handle time.Duration fields.
func (c *TaskQueueConfig) UnmarshalJSON(data []byte) error {
	type Alias TaskQueueConfig
	aux := &struct {
		WorkerStopTimeout            string `json:"worker_stop_timeout,omitempty"`
		StickyScheduleToStartTimeout string `json:"sticky_schedule_to_start_timeout,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Parse time durations if provided
	if aux.WorkerStopTimeout != "" {
		_, err := time.ParseDuration(aux.WorkerStopTimeout)
		if err != nil {
			return fmt.Errorf("invalid worker stop timeout format: %w", err)
		}
		c.WorkerStopTimeout = aux.WorkerStopTimeout
	}

	if aux.StickyScheduleToStartTimeout != "" {
		_, err := time.ParseDuration(aux.StickyScheduleToStartTimeout)
		if err != nil {
			return fmt.Errorf("invalid sticky schedule to start timeout format: %w", err)
		}
		c.StickyScheduleToStartTimeout = aux.StickyScheduleToStartTimeout
	}

	return nil
}

// InitDefaults initializes the TaskQueueConfig with default values
func (c *TaskQueueConfig) InitDefaults() {
	// Default concurrency settings
	if c.MaxConcurrentActivityExecution <= 0 {
		c.MaxConcurrentActivityExecution = 1000 // defaultMaxConcurrentActivityExecutionSize
	}
	if c.MaxConcurrentWorkflowExecution <= 0 {
		c.MaxConcurrentWorkflowExecution = 1000 // defaultMaxConcurrentTaskExecutionSize
	}
	if c.MaxConcurrentLocalActivityExecution <= 0 {
		c.MaxConcurrentLocalActivityExecution = 1000
	}
	if c.StickyScheduleToStartTimeout == "" {
		c.StickyScheduleToStartTimeout = "5s"
	}
	if c.WorkerStopTimeout == "" {
		c.WorkerStopTimeout = "0s"
	}
	if c.MaxConcurrentActivityPollers <= 0 {
		c.MaxConcurrentActivityPollers = 2
	}
	if c.MaxConcurrentWorkflowPollers <= 0 {
		c.MaxConcurrentWorkflowPollers = 2
	}
	if c.MaxConcurrentSessionExecutionSize <= 0 {
		c.MaxConcurrentSessionExecutionSize = 1000
	}

	// Initialize lifecycle defaults
	c.Lifecycle.InitDefaults()
}

// Validate checks if the task queue configuration is valid
func (c *TaskQueueConfig) Validate() error {
	// Client reference is required
	if c.Client.String() == "" {
		return fmt.Errorf("client reference cannot be empty")
	}

	// Task queue name is required
	if c.TaskQueue == "" {
		return fmt.Errorf("task queue name cannot be empty")
	}

	// Validate concurrency settings
	if c.MaxConcurrentActivityExecution <= 0 {
		return fmt.Errorf("max concurrent activity execution must be positive")
	}
	if c.MaxConcurrentWorkflowExecution <= 0 {
		return fmt.Errorf("max concurrent workflow execution must be positive")
	}
	if c.MaxConcurrentWorkflowPollers == 1 {
		return fmt.Errorf("max concurrent workflow task pollers cannot be 1 due to sticky/non-sticky queue logic")
	}

	// Validate rate limits if set
	if c.WorkerActivitiesPerSecond < 0 {
		return fmt.Errorf("worker activities per second cannot be negative")
	}
	if c.WorkerLocalActivitiesPerSecond < 0 {
		return fmt.Errorf("worker local activities per second cannot be negative")
	}
	if c.TaskQueueActivitiesPerSecond < 0 {
		return fmt.Errorf("task queue activities per second cannot be negative")
	}

	// Cannot disable both workflow and activity workers
	if c.DisableWorkflowWorker && c.LocalActivityWorkerOnly {
		return fmt.Errorf("cannot set both DisableWorkflowWorker and LocalActivityWorkerOnly")
	}

	// Validate durations if provided
	if c.StickyScheduleToStartTimeout != "" {
		_, err := time.ParseDuration(c.StickyScheduleToStartTimeout)
		if err != nil {
			return fmt.Errorf("invalid sticky schedule to start timeout: %w", err)
		}
	}

	if c.WorkerStopTimeout != "" {
		_, err := time.ParseDuration(c.WorkerStopTimeout)
		if err != nil {
			return fmt.Errorf("invalid worker stop timeout: %w", err)
		}
	}

	return nil
}

// ToWorkerOptions converts the configuration to Temporal worker options
func (c *TaskQueueConfig) ToWorkerOptions() worker.Options {
	var stickyTimeout time.Duration
	if c.StickyScheduleToStartTimeout != "" {
		duration, _ := time.ParseDuration(c.StickyScheduleToStartTimeout)
		stickyTimeout = duration
	}

	var workerStopTimeout time.Duration
	if c.WorkerStopTimeout != "" {
		duration, _ := time.ParseDuration(c.WorkerStopTimeout)
		workerStopTimeout = duration
	}

	return worker.Options{
		MaxConcurrentActivityExecutionSize:      c.MaxConcurrentActivityExecution,
		MaxConcurrentWorkflowTaskExecutionSize:  c.MaxConcurrentWorkflowExecution,
		MaxConcurrentLocalActivityExecutionSize: c.MaxConcurrentLocalActivityExecution,
		WorkerActivitiesPerSecond:               c.WorkerActivitiesPerSecond,
		WorkerLocalActivitiesPerSecond:          c.WorkerLocalActivitiesPerSecond,
		TaskQueueActivitiesPerSecond:            c.TaskQueueActivitiesPerSecond,
		MaxConcurrentActivityTaskPollers:        c.MaxConcurrentActivityPollers,
		MaxConcurrentWorkflowTaskPollers:        c.MaxConcurrentWorkflowPollers,
		EnableLoggingInReplay:                   c.EnableLoggingInReplay,
		StickyScheduleToStartTimeout:            stickyTimeout,
		DisableWorkflowWorker:                   c.DisableWorkflowWorker,
		LocalActivityWorkerOnly:                 c.LocalActivityWorkerOnly,
		WorkerStopTimeout:                       workerStopTimeout,
		EnableSessionWorker:                     c.EnableSessionWorker,
		MaxConcurrentSessionExecutionSize:       c.MaxConcurrentSessionExecutionSize,
		DisableEagerActivities:                  c.DisableEagerActivities,
		MaxConcurrentEagerActivityExecutionSize: c.MaxConcurrentEagerActivityExecution,
		BackgroundActivityContext:               nil, // To be set at runtime
	}
}

// GetLifecycleConfig returns the lifecycle configuration with client dependency
func (c *TaskQueueConfig) GetLifecycleConfig() supervisor.LifecycleConfig {
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
