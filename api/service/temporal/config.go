package temporal

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.temporal.io/sdk/worker"
)

const (
	System        events.System = "temporal"
	KindClient    registry.Kind = "temporal.client"
	KindTaskQueue registry.Kind = "temporal.task_queue"
	KindActivity  registry.Kind = "temporal.activity_definition"
	KindWorkflow  registry.Kind = "temporal.workflow_definition"
)

type ClientAuthType string

const (
	NoClientCert               ClientAuthType = "no_client_cert"
	RequestClientCert          ClientAuthType = "request_client_cert"
	RequireAnyClientCert       ClientAuthType = "require_any_client_cert"
	VerifyClientCertIfGiven    ClientAuthType = "verify_client_cert_if_given"
	RequireAndVerifyClientCert ClientAuthType = "require_and_verify_client_cert"
)

// ClientConfig represents configuration for a Temporal client connection
type ClientConfig struct {
	Address   string     `json:"address"`
	Namespace string     `json:"namespace"`
	TLS       *TLSConfig `json:"tls,omitempty"`
	CacheSize int        `json:"cache_size"`
}

// TLSConfig represents TLS/SSL configuration
type TLSConfig struct {
	Key        string         `json:"key"`
	Cert       string         `json:"cert"`
	RootCA     string         `json:"root_ca"`
	AuthType   ClientAuthType `json:"client_auth_type"`
	ServerName string         `json:"server_name"`
	UseH2C     bool           `json:"use_h2c"`
}

type ActivityConfig struct {
	TaskQueue registry.ID `json:"task_queue"`
	Function  registry.ID `json:"function"`
}

// TaskQueueConfig represents configuration for a Temporal task queue
type TaskQueueConfig struct {
	Client                           registry.ID                `json:"client"`
	TaskQueue                        string                     `json:"task_queue"` // temporal level task queue
	MaxConcurrentActivityExecution   int                        `json:"max_concurrent_activity_execution"`
	WorkerActivitiesPerSecond        float64                    `json:"worker_activities_per_second"`
	MaxConcurrentLocalActivity       int                        `json:"max_concurrent_local_activity"`
	WorkerLocalActivitiesPerSecond   float64                    `json:"worker_local_activities_per_second"`
	TaskQueueActivitiesPerSecond     float64                    `json:"task_queue_activities_per_second"`
	MaxConcurrentActivityPollers     int                        `json:"max_concurrent_activity_pollers"`
	MaxConcurrentWorkflowExecution   int                        `json:"max_concurrent_workflow_execution"`
	MaxConcurrentWorkflowPollers     int                        `json:"max_concurrent_workflow_pollers"`
	StickyScheduleTimeout            time.Duration              `json:"sticky_schedule_timeout"`
	EnableLoggingReplay              bool                       `json:"enable_logging_replay"`
	WorkerStopTimeout                time.Duration              `json:"worker_stop_timeout"`
	MaxHeartbeatThrottleInterval     time.Duration              `json:"max_heartbeat_throttle_interval"`
	DefaultHeartbeatThrottleInterval time.Duration              `json:"default_heartbeat_throttle_interval"`
	EnableSessionWorker              bool                       `json:"enable_session_worker"`
	MaxConcurrentSessionExecution    int                        `json:"max_concurrent_session_execution"`
	DisableWorkflowWorker            bool                       `json:"disable_workflow_worker"`
	LocalActivityWorkerOnly          bool                       `json:"local_activity_worker_only"`
	DeadlockDetectionTimeout         time.Duration              `json:"deadlock_detection_timeout"`
	DisableEagerActivities           bool                       `json:"disable_eager_activities"`
	MaxConcurrentEagerActivities     int                        `json:"max_concurrent_eager_activities"`
	DisableRegistrationAliasing      bool                       `json:"disable_registration_aliasing"`
	Lifecycle                        supervisor.LifecycleConfig `json:"lifecycle"`
}

// UnmarshalJSON provides custom unmarshaling for TaskQueueConfig
func (c *TaskQueueConfig) UnmarshalJSON(data []byte) error {
	type Alias TaskQueueConfig
	aux := &struct {
		Client                           string `json:"client"`
		StickyScheduleTimeout            string `json:"sticky_schedule_timeout"`
		WorkerStopTimeout                string `json:"worker_stop_timeout"`
		MaxHeartbeatThrottleInterval     string `json:"max_heartbeat_throttle_interval"`
		DefaultHeartbeatThrottleInterval string `json:"default_heartbeat_throttle_interval"`
		DeadlockDetectionTimeout         string `json:"deadlock_detection_timeout"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Handle client ID conversion
	if aux.Client != "" {
		c.Client = registry.ID(aux.Client)
	}

	var err error
	if aux.StickyScheduleTimeout != "" {
		if c.StickyScheduleTimeout, err = time.ParseDuration(aux.StickyScheduleTimeout); err != nil {
			return fmt.Errorf("invalid StickyScheduleTimeout duration format: %w", err)
		}
	}

	if aux.WorkerStopTimeout != "" {
		if c.WorkerStopTimeout, err = time.ParseDuration(aux.WorkerStopTimeout); err != nil {
			return fmt.Errorf("invalid WorkerStopTimeout duration format: %w", err)
		}
	}

	if aux.MaxHeartbeatThrottleInterval != "" {
		if c.MaxHeartbeatThrottleInterval, err = time.ParseDuration(aux.MaxHeartbeatThrottleInterval); err != nil {
			return fmt.Errorf("invalid MaxHeartbeatThrottleInterval duration format: %w", err)
		}
	}

	if aux.DefaultHeartbeatThrottleInterval != "" {
		if c.DefaultHeartbeatThrottleInterval, err = time.ParseDuration(aux.DefaultHeartbeatThrottleInterval); err != nil {
			return fmt.Errorf("invalid DefaultHeartbeatThrottleInterval duration format: %w", err)
		}
	}

	if aux.DeadlockDetectionTimeout != "" {
		if c.DeadlockDetectionTimeout, err = time.ParseDuration(aux.DeadlockDetectionTimeout); err != nil {
			return fmt.Errorf("invalid DeadlockDetectionTimeout duration format: %w", err)
		}
	}

	return nil
}

// MarshalJSON provides custom marshaling for TaskQueueConfig
func (c *TaskQueueConfig) MarshalJSON() ([]byte, error) {
	type Alias TaskQueueConfig
	return json.Marshal(&struct {
		Client                           string `json:"client"`
		StickyScheduleTimeout            string `json:"sticky_schedule_timeout"`
		WorkerStopTimeout                string `json:"worker_stop_timeout"`
		MaxHeartbeatThrottleInterval     string `json:"max_heartbeat_throttle_interval"`
		DefaultHeartbeatThrottleInterval string `json:"default_heartbeat_throttle_interval"`
		DeadlockDetectionTimeout         string `json:"deadlock_detection_timeout"`
		*Alias
	}{
		Client:                           string(c.Client),
		StickyScheduleTimeout:            c.StickyScheduleTimeout.String(),
		WorkerStopTimeout:                c.WorkerStopTimeout.String(),
		MaxHeartbeatThrottleInterval:     c.MaxHeartbeatThrottleInterval.String(),
		DefaultHeartbeatThrottleInterval: c.DefaultHeartbeatThrottleInterval.String(),
		DeadlockDetectionTimeout:         c.DeadlockDetectionTimeout.String(),
		Alias:                            (*Alias)(c),
	})
}

// Validate validates the TaskQueueConfig
func (c *TaskQueueConfig) Validate() error {
	if c.Client == "" {
		return fmt.Errorf("client is required")
	}

	if c.TaskQueue == "" {
		return fmt.Errorf("task_queue is required")
	}

	if c.MaxConcurrentActivityExecution < 0 {
		return fmt.Errorf("max_concurrent_activity_execution must be non-negative")
	}
	if c.WorkerActivitiesPerSecond < 0 {
		return fmt.Errorf("worker_activities_per_second must be non-negative")
	}
	if c.MaxConcurrentLocalActivity < 0 {
		return fmt.Errorf("max_concurrent_local_activity must be non-negative")
	}
	if c.MaxConcurrentWorkflowExecution < 0 {
		return fmt.Errorf("max_concurrent_workflow_execution must be non-negative")
	}
	if c.MaxConcurrentEagerActivities < 0 {
		return fmt.Errorf("max_concurrent_eager_activities must be non-negative")
	}

	if c.StickyScheduleTimeout < 0 {
		return fmt.Errorf("sticky_schedule_timeout must be non-negative")
	}
	if c.WorkerStopTimeout < 0 {
		return fmt.Errorf("worker_stop_timeout must be non-negative")
	}
	if c.MaxHeartbeatThrottleInterval < 0 {
		return fmt.Errorf("max_heartbeat_throttle_interval must be non-negative")
	}
	if c.DefaultHeartbeatThrottleInterval < 0 {
		return fmt.Errorf("default_heartbeat_throttle_interval must be non-negative")
	}
	if c.DeadlockDetectionTimeout < 0 {
		return fmt.Errorf("deadlock_detection_timeout must be non-negative")
	}

	return nil
}

// ToWorkerOptions converts TaskQueueConfig to worker.Options
func (c *TaskQueueConfig) ToWorkerOptions() worker.Options {
	return worker.Options{
		MaxConcurrentActivityExecutionSize:      c.MaxConcurrentActivityExecution,
		WorkerActivitiesPerSecond:               c.WorkerActivitiesPerSecond,
		MaxConcurrentLocalActivityExecutionSize: c.MaxConcurrentLocalActivity,
		WorkerLocalActivitiesPerSecond:          c.WorkerLocalActivitiesPerSecond,
		TaskQueueActivitiesPerSecond:            c.TaskQueueActivitiesPerSecond,
		MaxConcurrentActivityTaskPollers:        c.MaxConcurrentActivityPollers,
		MaxConcurrentWorkflowTaskExecutionSize:  c.MaxConcurrentWorkflowExecution,
		MaxConcurrentWorkflowTaskPollers:        c.MaxConcurrentWorkflowPollers,
		EnableLoggingInReplay:                   c.EnableLoggingReplay,
		StickyScheduleToStartTimeout:            c.StickyScheduleTimeout,
		WorkerStopTimeout:                       c.WorkerStopTimeout,
		MaxHeartbeatThrottleInterval:            c.MaxHeartbeatThrottleInterval,
		DefaultHeartbeatThrottleInterval:        c.DefaultHeartbeatThrottleInterval,
		EnableSessionWorker:                     c.EnableSessionWorker,
		MaxConcurrentSessionExecutionSize:       c.MaxConcurrentSessionExecution,
		DisableWorkflowWorker:                   c.DisableWorkflowWorker,
		LocalActivityWorkerOnly:                 c.LocalActivityWorkerOnly,
		DeadlockDetectionTimeout:                c.DeadlockDetectionTimeout,
		DisableEagerActivities:                  c.DisableEagerActivities,
		MaxConcurrentEagerActivityExecutionSize: c.MaxConcurrentEagerActivities,
		DisableRegistrationAliasing:             c.DisableRegistrationAliasing,
	}
}

// UnmarshalJSON provides custom unmarshaling for ActivityConfig
func (c *ActivityConfig) UnmarshalJSON(data []byte) error {
	type Alias ActivityConfig
	aux := &struct {
		TaskQueue string `json:"task_queue"`
		Function  string `json:"function"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Convert string to registry.ID
	if aux.TaskQueue != "" {
		c.TaskQueue = registry.ID(aux.TaskQueue)
	}
	if aux.Function != "" {
		c.Function = registry.ID(aux.Function)
	}

	return nil
}

// MarshalJSON provides custom marshaling for ActivityConfig
func (c *ActivityConfig) MarshalJSON() ([]byte, error) {
	type Alias ActivityConfig
	return json.Marshal(&struct {
		TaskQueue string `json:"task_queue"`
		Function  string `json:"function"`
		*Alias
	}{
		TaskQueue: string(c.TaskQueue),
		Function:  string(c.Function),
		Alias:     (*Alias)(c),
	})
}

// Validate validates the ActivityConfig
func (c *ActivityConfig) Validate() error {
	if c.TaskQueue == "" {
		return fmt.Errorf("task_queue is required")
	}

	if c.Function == "" {
		return fmt.Errorf("function is required")
	}

	return nil
}
