package temporal

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

const (
	Worker registry.Kind = "temporal.worker"
)

// WorkerConfig represents the configuration for a Temporal worker
type WorkerConfig struct {
	Meta          attrs.Bag                  `json:"meta"`
	Client        registry.ID                `json:"client"`
	TaskQueue     string                     `json:"task_queue"`
	Lifecycle     supervisor.LifecycleConfig `json:"lifecycle"`
	WorkerOptions WorkerOptionsConfig        `json:"worker_options"`
}

// WorkerOptionsConfig represents configuration options for a Temporal worker
type WorkerOptionsConfig struct {
	Identity                                string             `json:"identity,omitempty"`
	DefaultVersioningBehavior               VersioningBehavior `json:"default_versioning_behavior,omitempty"`
	BuildIDEnv                              string             `json:"build_id_env,omitempty"`
	BuildID                                 string             `json:"build_id,omitempty"`
	DeploymentName                          string             `json:"deployment_name,omitempty"`
	DeadlockDetectionTimeout                time.Duration      `json:"deadlock_detection_timeout,omitzero,format:units"`
	MaxConcurrentActivityTaskPollers        int                `json:"max_concurrent_activity_task_pollers,omitempty"`
	WorkerActivitiesPerSecond               float64            `json:"worker_activities_per_second,omitempty"`
	WorkerLocalActivitiesPerSecond          float64            `json:"worker_local_activities_per_second,omitempty"`
	TaskQueueActivitiesPerSecond            float64            `json:"task_queue_activities_per_second,omitempty"`
	StickyScheduleToStartTimeout            time.Duration      `json:"sticky_schedule_to_start_timeout,omitzero,format:units"`
	WorkerStopTimeout                       time.Duration      `json:"worker_stop_timeout,omitzero,format:units"`
	MaxConcurrentActivityExecutionSize      int                `json:"max_concurrent_activity_execution_size,omitempty"`
	MaxHeartbeatThrottleInterval            time.Duration      `json:"max_heartbeat_throttle_interval,omitzero,format:units"`
	DefaultHeartbeatThrottleInterval        time.Duration      `json:"default_heartbeat_throttle_interval,omitzero,format:units"`
	MaxConcurrentWorkflowTaskExecutionSize  int                `json:"max_concurrent_workflow_task_execution_size,omitempty"`
	MaxConcurrentLocalActivityExecutionSize int                `json:"max_concurrent_local_activity_execution_size,omitempty"`
	MaxConcurrentSessionExecutionSize       int                `json:"max_concurrent_session_execution_size,omitempty"`
	MaxConcurrentEagerActivityExecutionSize int                `json:"max_concurrent_eager_activity_execution_size,omitempty"`
	MaxConcurrentWorkflowTaskPollers        int                `json:"max_concurrent_workflow_task_pollers,omitempty"`
	DisableRegistrationAliasing             bool               `json:"disable_registration_aliasing,omitempty"`
	DisableEagerActivities                  bool               `json:"disable_eager_activities,omitempty"`
	LocalActivityWorkerOnly                 bool               `json:"local_activity_worker_only,omitempty"`
	DisableWorkflowWorker                   bool               `json:"disable_workflow_worker,omitempty"`
	EnableSessionWorker                     bool               `json:"enable_session_worker,omitempty"`
	UseVersioning                           bool               `json:"use_versioning,omitempty"`
	EnableLoggingInReplay                   bool               `json:"enable_logging_in_replay,omitempty"`
}

// VersioningBehavior specifies how workflows handle version changes.
type VersioningBehavior string

const (
	// VersioningBehaviorPinned keeps workflow on the same build ID until completion.
	VersioningBehaviorPinned VersioningBehavior = "pinned"
	// VersioningBehaviorAutoUpgrade moves workflow to latest version on next task.
	VersioningBehaviorAutoUpgrade VersioningBehavior = "auto_upgrade"
)

// InitDefaults initializes default values for WorkerConfig
func (c *WorkerConfig) InitDefaults() {
	c.Lifecycle.InitDefaults()

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
	if c.WorkerOptions.StickyScheduleToStartTimeout == 0 {
		c.WorkerOptions.StickyScheduleToStartTimeout = 5 * time.Second
	}
	if c.WorkerOptions.MaxConcurrentActivityTaskPollers <= 0 {
		c.WorkerOptions.MaxConcurrentActivityTaskPollers = 20
	}
	if c.WorkerOptions.MaxConcurrentWorkflowTaskPollers <= 0 {
		c.WorkerOptions.MaxConcurrentWorkflowTaskPollers = 20
	}
}

// Validate checks if the worker configuration is valid
func (c *WorkerConfig) Validate() error {
	if c.Client.String() == "" {
		return ErrClientReferenceEmpty
	}

	if c.TaskQueue == "" {
		return ErrTaskQueueEmpty
	}

	if c.WorkerOptions.MaxConcurrentActivityExecutionSize <= 0 {
		return ErrMaxConcurrentActivityInvalid
	}
	if c.WorkerOptions.MaxConcurrentWorkflowTaskExecutionSize <= 0 {
		return ErrMaxConcurrentWorkflowInvalid
	}
	if c.WorkerOptions.MaxConcurrentWorkflowTaskPollers == 1 {
		return ErrMaxConcurrentWorkflowTaskPollersInvalid
	}

	if c.WorkerOptions.WorkerActivitiesPerSecond < 0 {
		return ErrWorkerActivitiesPerSecondInvalid
	}
	if c.WorkerOptions.WorkerLocalActivitiesPerSecond < 0 {
		return ErrWorkerLocalActivitiesPerSecondInvalid
	}
	if c.WorkerOptions.TaskQueueActivitiesPerSecond < 0 {
		return ErrTaskQueueActivitiesPerSecondInvalid
	}

	if c.WorkerOptions.DisableWorkflowWorker && c.WorkerOptions.LocalActivityWorkerOnly {
		return ErrDisableWorkflowWorkerConflict
	}

	return nil
}

// UnmarshalJSON implements custom unmarshaling for WorkerOptionsConfig to parse duration strings.
func (c *WorkerOptionsConfig) UnmarshalJSON(data []byte) error {
	type Alias WorkerOptionsConfig
	aux := &struct {
		*Alias
		StickyScheduleToStartTimeout     string `json:"sticky_schedule_to_start_timeout"`
		WorkerStopTimeout                string `json:"worker_stop_timeout"`
		DeadlockDetectionTimeout         string `json:"deadlock_detection_timeout"`
		MaxHeartbeatThrottleInterval     string `json:"max_heartbeat_throttle_interval"`
		DefaultHeartbeatThrottleInterval string `json:"default_heartbeat_throttle_interval"`
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if aux.StickyScheduleToStartTimeout != "" {
		d, err := time.ParseDuration(aux.StickyScheduleToStartTimeout)
		if err != nil {
			return fmt.Errorf("invalid sticky_schedule_to_start_timeout: %w", err)
		}
		c.StickyScheduleToStartTimeout = d
	}
	if aux.WorkerStopTimeout != "" {
		d, err := time.ParseDuration(aux.WorkerStopTimeout)
		if err != nil {
			return fmt.Errorf("invalid worker_stop_timeout: %w", err)
		}
		c.WorkerStopTimeout = d
	}
	if aux.DeadlockDetectionTimeout != "" {
		d, err := time.ParseDuration(aux.DeadlockDetectionTimeout)
		if err != nil {
			return fmt.Errorf("invalid deadlock_detection_timeout: %w", err)
		}
		c.DeadlockDetectionTimeout = d
	}
	if aux.MaxHeartbeatThrottleInterval != "" {
		d, err := time.ParseDuration(aux.MaxHeartbeatThrottleInterval)
		if err != nil {
			return fmt.Errorf("invalid max_heartbeat_throttle_interval: %w", err)
		}
		c.MaxHeartbeatThrottleInterval = d
	}
	if aux.DefaultHeartbeatThrottleInterval != "" {
		d, err := time.ParseDuration(aux.DefaultHeartbeatThrottleInterval)
		if err != nil {
			return fmt.Errorf("invalid default_heartbeat_throttle_interval: %w", err)
		}
		c.DefaultHeartbeatThrottleInterval = d
	}
	return nil
}
