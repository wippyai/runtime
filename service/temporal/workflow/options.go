package workflow

import (
	"fmt"
	"time"

	commonpb "go.temporal.io/api/common/v1"
	bindings "go.temporal.io/sdk/internalbindings"
	"google.golang.org/protobuf/types/known/durationpb"
)

// ActivityOptions configures activity execution.
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

// LocalActivityOptions configures local activity execution.
type LocalActivityOptions struct {
	ScheduleToClose string       `json:"schedule_to_close_timeout,omitempty"`
	StartToClose    string       `json:"start_to_close_timeout,omitempty"`
	RetryPolicy     *RetryPolicy `json:"retry_policy,omitempty"`
}

// ChildWorkflowOptions configures child workflow execution.
type ChildWorkflowOptions struct {
	WorkflowID          string       `json:"workflow_id,omitempty"`
	TaskQueue           string       `json:"task_queue,omitempty"`
	ExecutionTimeout    string       `json:"execution_timeout,omitempty"`
	RunTimeout          string       `json:"run_timeout,omitempty"`
	TaskTimeout         string       `json:"task_timeout,omitempty"`
	RetryPolicy         *RetryPolicy `json:"retry_policy,omitempty"`
	WaitForCancellation bool         `json:"wait_for_cancellation,omitempty"`
	ParentClosePolicy   string       `json:"parent_close_policy,omitempty"`
}

// RetryPolicy defines retry behavior.
type RetryPolicy struct {
	InitialInterval    string   `json:"initial_interval,omitempty"`
	BackoffCoefficient float64  `json:"backoff_coefficient,omitempty"`
	MaximumInterval    string   `json:"maximum_interval,omitempty"`
	MaximumAttempts    float64  `json:"maximum_attempts,omitempty"`
	NonRetryableErrors []string `json:"non_retryable_errors,omitempty"`
}

// ToExecuteActivityOptions converts to Temporal SDK options.
func (o *ActivityOptions) ToExecuteActivityOptions() (bindings.ExecuteActivityOptions, error) {
	var opts bindings.ExecuteActivityOptions

	if o == nil {
		return opts, nil
	}

	if o.ScheduleToClose != "" {
		d, err := time.ParseDuration(o.ScheduleToClose)
		if err != nil {
			return opts, fmt.Errorf("invalid ScheduleToClose duration: %w", err)
		}
		opts.ScheduleToCloseTimeout = d
	}

	if o.ScheduleToStart != "" {
		d, err := time.ParseDuration(o.ScheduleToStart)
		if err != nil {
			return opts, fmt.Errorf("invalid ScheduleToStart duration: %w", err)
		}
		opts.ScheduleToStartTimeout = d
	}

	if o.StartToClose != "" {
		d, err := time.ParseDuration(o.StartToClose)
		if err != nil {
			return opts, fmt.Errorf("invalid StartToClose duration: %w", err)
		}
		opts.StartToCloseTimeout = d
	}

	if o.HeartbeatTimeout != "" {
		d, err := time.ParseDuration(o.HeartbeatTimeout)
		if err != nil {
			return opts, fmt.Errorf("invalid HeartbeatTimeout duration: %w", err)
		}
		opts.HeartbeatTimeout = d
	}

	opts.ActivityID = o.ActivityID
	opts.TaskQueueName = o.TaskQueue
	opts.WaitForCancellation = o.WaitForCancellation
	opts.DisableEagerExecution = o.DisableEagerExecution
	opts.Summary = o.Summary

	if o.RetryPolicy != nil {
		rp, err := o.RetryPolicy.ToCommonRetryPolicy()
		if err != nil {
			return opts, err
		}
		opts.RetryPolicy = rp
	}

	return opts, nil
}

// ToLocalActivityOptions converts to Temporal SDK local activity options.
// Note: RetryPolicy for local activities is not supported through this interface
// as it requires internal SDK types.
func (o *LocalActivityOptions) ToLocalActivityOptions() (bindings.ExecuteLocalActivityOptions, error) {
	var opts bindings.ExecuteLocalActivityOptions

	if o == nil {
		return opts, nil
	}

	if o.ScheduleToClose != "" {
		d, err := time.ParseDuration(o.ScheduleToClose)
		if err != nil {
			return opts, fmt.Errorf("invalid ScheduleToClose duration: %w", err)
		}
		opts.ScheduleToCloseTimeout = d
	}

	if o.StartToClose != "" {
		d, err := time.ParseDuration(o.StartToClose)
		if err != nil {
			return opts, fmt.Errorf("invalid StartToClose duration: %w", err)
		}
		opts.StartToCloseTimeout = d
	}

	return opts, nil
}

// ToCommonRetryPolicy converts to Temporal common retry policy.
func (rp *RetryPolicy) ToCommonRetryPolicy() (*commonpb.RetryPolicy, error) {
	if rp == nil {
		return nil, nil
	}

	policy := &commonpb.RetryPolicy{
		BackoffCoefficient:     rp.BackoffCoefficient,
		MaximumAttempts:        int32(rp.MaximumAttempts),
		NonRetryableErrorTypes: rp.NonRetryableErrors,
	}

	if rp.InitialInterval != "" {
		d, err := time.ParseDuration(rp.InitialInterval)
		if err != nil {
			return nil, fmt.Errorf("invalid RetryPolicy.InitialInterval duration: %w", err)
		}
		policy.InitialInterval = &durationpb.Duration{Seconds: int64(d.Seconds())}
	}

	if rp.MaximumInterval != "" {
		d, err := time.ParseDuration(rp.MaximumInterval)
		if err != nil {
			return nil, fmt.Errorf("invalid RetryPolicy.MaximumInterval duration: %w", err)
		}
		policy.MaximumInterval = &durationpb.Duration{Seconds: int64(d.Seconds())}
	}

	return policy, nil
}
