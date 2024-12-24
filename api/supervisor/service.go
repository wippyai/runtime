package supervisor

import (
	"context"
	"github.com/ponyruntime/pony/api/events"
	"time"
)

const (
	System   events.System = "supervisor"
	Register events.Kind   = "supervisor.service.register"
	Remove   events.Kind   = "supervisor.service.remove"
	Update   events.Kind   = "supervisor.service.status"

	// Service lifecycle events
	Start events.Kind = "supervisor.service.start"
	Stop  events.Kind = "supervisor.service.stop"

	// Unknown indicates the service status is currently unknown.
	Unknown Status = "unknown"
	// Starting indicates the service is currently starting up.
	Starting Status = "starting"
	// Running indicates the service is currently running and operational.
	Running Status = "running"
	// Stopping indicates the service is in the process of a graceful shutdown.
	Stopping Status = "stopping"
	// Stopped indicates the service has stopped and is no longer running.
	Stopped Status = "stopped"
	// Failed indicates the service has failed and is not running.
	Failed Status = "failed"
)

type (
	// Entry payload for supervisor registration event. Service will be identified by event path.
	Entry struct {
		Service Service
		Config  ServiceConfig
	}

	// Status represents the operational status of a service.
	Status string

	// ServiceConfig defines the configuration for a service managed by the supervisor.
	ServiceConfig struct {
		// AutoStart determines if the service should start automatically when the supervisor starts.
		AutoStart bool `json:"auto_start" yaml:"auto_start" default:"false"`
		// StartTimeout specifies the maximum duration allowed for the service to start.
		StartTimeout time.Duration `json:"start_timeout" yaml:"start_timeout" default:"30s"`
		// StopTimeout specifies the maximum duration allowed for the service to stop.
		StopTimeout time.Duration `json:"stop_timeout" yamal:"stop_timeout" default:"30s"`
		// RetryPolicy defines the policy for retrying a failed service.
		RetryPolicy RetryPolicy `json:"restart" yaml:"restart"`
		// DependsOn specifies a list of service names that this service depends on.
		DependsOn []string `json:"depends_on" yaml:"depends_on" default:"[]"` // Empty array
	}

	// RetryPolicy defines the parameters for retrying a service after a failure.
	RetryPolicy struct {
		// InitialDelay specifies the initial delay before the first retry attempt.
		InitialDelay time.Duration `json:"initial_delay" yaml:"initial_delay" default:"1s"`
		// MaxDelay specifies the maximum delay between retry attempts.
		MaxDelay time.Duration `json:"max_delay" yaml:"max_delay" default:"30s"`
		// BackoffFactor determines the exponential backoff factor for increasing the delay between retries.
		BackoffFactor float64 `json:"backoff_factor" yaml:"backoff_factor" default:"2.0"`
		// Jitter introduces random variation to the retry delay to prevent synchronized retries.
		Jitter float64 `json:"jitter" yaml:"jitter" default:"0.1"`
		// MaxAttempts specifies the maximum number of retry attempts before giving up.
		MaxAttempts int `json:"max_attempts" yaml:"max_attempts" default:"5"`
	}

	// Service defines the interface that must be implemented by any service managed by the supervisor.
	Service interface {
		// Start initiates the service. Service can post current status to the returned channel.
		// The context passed into start method is primary service context, service must exit if context is done.
		Start(ctx context.Context) (<-chan any, error)
		// Stop terminates the service. The context passed into stop method is only for graceful stop, service must return error
		// if it cannot stop within the context deadline.
		Stop(ctx context.Context) error
	}
)
