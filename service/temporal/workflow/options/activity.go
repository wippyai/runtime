package options

import (
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/internalbindings"
	"google.golang.org/protobuf/types/known/durationpb"
	"time"
)

// ActivityOptions represents the configurable parameters for an activity execution
type ActivityOptions struct {
	ActivityID            string       `json:"activity_id,omitempty"`
	TaskQueue             string       `json:"task_queue,omitempty"`
	ScheduleToClose       string       `json:"schedule_to_close_timeout,omitempty"`
	ScheduleToStart       string       `json:"schedule_to_start_timeout,omitempty"`
	StartToClose          string       `json:"start_to_close_timeout,omitempty"`
	HeartbeatTimeout      string       `json:"heartbeat_timeout,omitempty"`
	WaitForCancellation   bool         `json:"wait_for_cancellation,omitempty"`
	RetryPolicy           *RetryPolicy `json:"retry_policy,omitempty"`
	DisableEagerExecution bool         `json:"disable_eager_execution,omitempty"`
	Summary               string       `json:"summary,omitempty"`
}

// RetryPolicy defines the retry behavior for activity execution
type RetryPolicy struct {
	InitialInterval    string   `json:"initial_interval,omitempty"`
	BackoffCoefficient float64  `json:"backoff_coefficient,omitempty"`
	MaximumInterval    string   `json:"maximum_interval,omitempty"`
	MaximumAttempts    float64  `json:"maximum_attempts,omitempty"`
	NonRetryableErrors []string `json:"non_retryable_errors,omitempty"`
}

// ToExecuteActivityOptions converts ActivityOptions to temporal's ExecuteActivityOptions
func (o *ActivityOptions) ToExecuteActivityOptions() (internalbindings.ExecuteActivityOptions, error) {
	var opts internalbindings.ExecuteActivityOptions

	// Convert duration strings to time.Duration
	if o.ScheduleToClose != "" {
		d, err := time.ParseDuration(o.ScheduleToClose)
		if err != nil {
			return opts, err
		}
		opts.ScheduleToCloseTimeout = d
	}

	if o.ScheduleToStart != "" {
		d, err := time.ParseDuration(o.ScheduleToStart)
		if err != nil {
			return opts, err
		}
		opts.ScheduleToStartTimeout = d
	}

	if o.StartToClose != "" {
		d, err := time.ParseDuration(o.StartToClose)
		if err != nil {
			return opts, err
		}
		opts.StartToCloseTimeout = d
	}

	if o.HeartbeatTimeout != "" {
		d, err := time.ParseDuration(o.HeartbeatTimeout)
		if err != nil {
			return opts, err
		}
		opts.HeartbeatTimeout = d
	}

	// Set other fields
	opts.ActivityID = o.ActivityID
	opts.TaskQueueName = o.TaskQueue
	opts.WaitForCancellation = o.WaitForCancellation
	opts.DisableEagerExecution = o.DisableEagerExecution
	opts.Summary = o.Summary

	// Convert retry policy if present
	if o.RetryPolicy != nil {
		retryPolicy := &commonpb.RetryPolicy{
			BackoffCoefficient:     o.RetryPolicy.BackoffCoefficient,
			MaximumAttempts:        int32(o.RetryPolicy.MaximumAttempts),
			NonRetryableErrorTypes: o.RetryPolicy.NonRetryableErrors,
		}

		if o.RetryPolicy.InitialInterval != "" {
			d, err := time.ParseDuration(o.RetryPolicy.InitialInterval)
			if err != nil {
				return opts, err
			}
			retryPolicy.InitialInterval = &durationpb.Duration{Seconds: int64(d.Seconds())}
		}

		if o.RetryPolicy.MaximumInterval != "" {
			d, err := time.ParseDuration(o.RetryPolicy.MaximumInterval)
			if err != nil {
				return opts, err
			}
			retryPolicy.MaximumInterval = &durationpb.Duration{Seconds: int64(d.Seconds())}
		}

		opts.RetryPolicy = retryPolicy
	}

	return opts, nil
}
