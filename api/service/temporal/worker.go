package temporal

import (
	"fmt"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
)

// Registry kind constants for Temporal components
const (
	// KindWorker identifies a temporal worker component
	KindWorker registry.Kind = "temporal.worker"
)

// WorkerConfig represents the configuration for a Temporal worker/task queue
type WorkerConfig struct {
	Meta          registry.Metadata          `json:"meta"`
	Client        registry.ID                `json:"client"`         // Reference to Temporal client
	TaskQueue     string                     `json:"task_queue"`     // Task queue name
	WorkerOptions WorkerOptionsConfig        `json:"worker_options"` // Temporal worker options
	Lifecycle     supervisor.LifecycleConfig `json:"lifecycle"`      // Lifecycle configuration
}

// WorkerOptionsConfig represents configuration options for a Temporal worker
type WorkerOptionsConfig struct {
	// Concurrency settings
	MaxConcurrentActivityExecutionSize      int `json:"max_concurrent_activity_execution_size,omitempty"`
	MaxConcurrentWorkflowTaskExecutionSize  int `json:"max_concurrent_workflow_task_execution_size,omitempty"`
	MaxConcurrentLocalActivityExecutionSize int `json:"max_concurrent_local_activity_execution_size,omitempty"`
	MaxConcurrentSessionExecutionSize       int `json:"max_concurrent_session_execution_size,omitempty"`
	MaxConcurrentEagerActivityExecutionSize int `json:"max_concurrent_eager_activity_execution_size,omitempty"`

	// Poller settings
	MaxConcurrentActivityTaskPollers int `json:"max_concurrent_activity_task_pollers,omitempty"`
	MaxConcurrentWorkflowTaskPollers int `json:"max_concurrent_workflow_task_pollers,omitempty"`

	// Rate limiting
	WorkerActivitiesPerSecond      float64 `json:"worker_activities_per_second,omitempty"`
	WorkerLocalActivitiesPerSecond float64 `json:"worker_local_activities_per_second,omitempty"`
	TaskQueueActivitiesPerSecond   float64 `json:"task_queue_activities_per_second,omitempty"`

	// Timeouts and intervals (as string durations, will be parsed)
	StickyScheduleToStartTimeout     string `json:"sticky_schedule_to_start_timeout,omitempty"`
	WorkerStopTimeout                string `json:"worker_stop_timeout,omitempty"`
	DeadlockDetectionTimeout         string `json:"deadlock_detection_timeout,omitempty"`
	MaxHeartbeatThrottleInterval     string `json:"max_heartbeat_throttle_interval,omitempty"`
	DefaultHeartbeatThrottleInterval string `json:"default_heartbeat_throttle_interval,omitempty"`

	// Feature flags
	EnableLoggingInReplay       bool `json:"enable_logging_in_replay,omitempty"`
	EnableSessionWorker         bool `json:"enable_session_worker,omitempty"`
	DisableWorkflowWorker       bool `json:"disable_workflow_worker,omitempty"`
	LocalActivityWorkerOnly     bool `json:"local_activity_worker_only,omitempty"`
	DisableEagerActivities      bool `json:"disable_eager_activities,omitempty"`
	DisableRegistrationAliasing bool `json:"disable_registration_aliasing,omitempty"`

	// Identity and versioning
	Identity                string `json:"identity,omitempty"`
	BuildID                 string `json:"build_id,omitempty"`
	UseBuildIDForVersioning bool   `json:"use_build_id_for_versioning,omitempty"`
}

// InitDefaults initializes default values for WorkerConfig
func (c *WorkerConfig) InitDefaults() {
	// Initialize Lifecycle defaults if not set
	c.Lifecycle.InitDefaults()

	// Initialize worker options defaults
	if c.WorkerOptions.MaxConcurrentActivityExecutionSize <= 0 {
		c.WorkerOptions.MaxConcurrentActivityExecutionSize = 1000
	}
	if c.WorkerOptions.MaxConcurrentWorkflowTaskExecutionSize <= 0 {
		c.WorkerOptions.MaxConcurrentWorkflowTaskExecutionSize = 1000
	}
	if c.WorkerOptions.MaxConcurrentLocalActivityExecutionSize <= 0 {
		c.WorkerOptions.MaxConcurrentLocalActivityExecutionSize = 1000
	}
	if c.WorkerOptions.MaxConcurrentSessionExecutionSize <= 0 {
		c.WorkerOptions.MaxConcurrentSessionExecutionSize = 1000
	}
	if c.WorkerOptions.StickyScheduleToStartTimeout == "" {
		c.WorkerOptions.StickyScheduleToStartTimeout = "5s"
	}
	if c.WorkerOptions.MaxConcurrentActivityTaskPollers <= 0 {
		c.WorkerOptions.MaxConcurrentActivityTaskPollers = 2
	}
	if c.WorkerOptions.MaxConcurrentWorkflowTaskPollers <= 0 {
		c.WorkerOptions.MaxConcurrentWorkflowTaskPollers = 2
	}
}

// Validate checks if the task queue configuration is valid
func (c *WorkerConfig) Validate() error {
	// Client reference is required
	if c.Client.String() == "" {
		return fmt.Errorf("client reference cannot be empty")
	}

	// Task queue name is required
	if c.TaskQueue == "" {
		return fmt.Errorf("task queue name cannot be empty")
	}

	// Validate worker options
	if c.WorkerOptions.MaxConcurrentActivityExecutionSize <= 0 {
		return fmt.Errorf("max concurrent activity execution must be positive")
	}
	if c.WorkerOptions.MaxConcurrentWorkflowTaskExecutionSize <= 0 {
		return fmt.Errorf("max concurrent workflow execution must be positive")
	}
	if c.WorkerOptions.MaxConcurrentWorkflowTaskPollers == 1 {
		return fmt.Errorf("max concurrent workflow task pollers cannot be 1 due to sticky/non-sticky queue logic")
	}

	// Validate rate limits if set
	if c.WorkerOptions.WorkerActivitiesPerSecond < 0 {
		return fmt.Errorf("worker activities per second cannot be negative")
	}
	if c.WorkerOptions.WorkerLocalActivitiesPerSecond < 0 {
		return fmt.Errorf("worker local activities per second cannot be negative")
	}
	if c.WorkerOptions.TaskQueueActivitiesPerSecond < 0 {
		return fmt.Errorf("task queue activities per second cannot be negative")
	}

	// Cannot disable both workflow and activity workers
	if c.WorkerOptions.DisableWorkflowWorker && c.WorkerOptions.LocalActivityWorkerOnly {
		return fmt.Errorf("cannot set both DisableWorkflowWorker and LocalActivityWorkerOnly")
	}

	// Validate all duration fields
	if c.WorkerOptions.StickyScheduleToStartTimeout != "" {
		if _, err := time.ParseDuration(c.WorkerOptions.StickyScheduleToStartTimeout); err != nil {
			return fmt.Errorf("invalid sticky schedule to start timeout: %w", err)
		}
	}

	if c.WorkerOptions.WorkerStopTimeout != "" {
		if _, err := time.ParseDuration(c.WorkerOptions.WorkerStopTimeout); err != nil {
			return fmt.Errorf("invalid worker stop timeout: %w", err)
		}
	}

	if c.WorkerOptions.DeadlockDetectionTimeout != "" {
		if _, err := time.ParseDuration(c.WorkerOptions.DeadlockDetectionTimeout); err != nil {
			return fmt.Errorf("invalid deadlock detection timeout: %w", err)
		}
	}

	if c.WorkerOptions.MaxHeartbeatThrottleInterval != "" {
		if _, err := time.ParseDuration(c.WorkerOptions.MaxHeartbeatThrottleInterval); err != nil {
			return fmt.Errorf("invalid max heartbeat throttle interval: %w", err)
		}
	}

	if c.WorkerOptions.DefaultHeartbeatThrottleInterval != "" {
		if _, err := time.ParseDuration(c.WorkerOptions.DefaultHeartbeatThrottleInterval); err != nil {
			return fmt.Errorf("invalid default heartbeat throttle interval: %w", err)
		}
	}

	return nil
}
