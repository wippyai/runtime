package workflow

import (
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/workflow/std"
	lua "github.com/yuin/gopher-lua"

	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	bindings "go.temporal.io/sdk/internalbindings"
	workflow "go.temporal.io/sdk/workflow"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	ActivityCommandType = "activity"
	TimerCommandType    = "timer.sleep"
)

type CommandHandlerFunc func(cmd runtime.Command, env bindings.WorkflowEnvironment, dc converter.DataConverter, transcoder payload.Transcoder) error

var commandHandlers = map[string]CommandHandlerFunc{
	ActivityCommandType: executeActivity,
	TimerCommandType:    executeTimer,
	std.TypeFuncsCall:   executeFuncs,
}

func RegisterCommandHandler(commandType string, handler CommandHandlerFunc) {
	commandHandlers[commandType] = handler
}

func ExecuteCommand(cmd runtime.Command, env bindings.WorkflowEnvironment, dc converter.DataConverter, transcoder payload.Transcoder) error {
	handler, exists := commandHandlers[cmd.Type()]
	if !exists {
		return fmt.Errorf("no handler registered for command type: %s", cmd.Type())
	}

	return handler(cmd, env, dc, transcoder)
}

func completeCommand(cmd runtime.Command, value payload.Payload, err error) {
	var result *runtime.Result
	if err != nil {
		result = &runtime.Result{Error: err}
	} else {
		result = &runtime.Result{Value: value}
	}

	if completeErr := cmd.Complete(result); completeErr != nil {
		zap.L().Error("failed to complete command",
			zap.String("command_id", cmd.ID()),
			zap.Error(completeErr))
	}
}

func extractStringParam(param payload.Payload) (string, error) {
	if str, ok := param.Data().(string); ok {
		return str, nil
	}

	if lv, ok := param.Data().(lua.LValue); ok {
		return lv.String(), nil
	}

	return "", fmt.Errorf("parameter must be a string")
}

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

type RetryPolicy struct {
	InitialInterval    string   `json:"initial_interval,omitempty"`
	BackoffCoefficient float64  `json:"backoff_coefficient,omitempty"`
	MaximumInterval    string   `json:"maximum_interval,omitempty"`
	MaximumAttempts    float64  `json:"maximum_attempts,omitempty"`
	NonRetryableErrors []string `json:"non_retryable_errors,omitempty"`
}

func (o *ActivityOptions) ToExecuteActivityOptions() (bindings.ExecuteActivityOptions, error) {
	var opts bindings.ExecuteActivityOptions

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
		retryPolicy := &commonpb.RetryPolicy{
			BackoffCoefficient:     o.RetryPolicy.BackoffCoefficient,
			MaximumAttempts:        int32(o.RetryPolicy.MaximumAttempts),
			NonRetryableErrorTypes: o.RetryPolicy.NonRetryableErrors,
		}

		if o.RetryPolicy.InitialInterval != "" {
			d, err := time.ParseDuration(o.RetryPolicy.InitialInterval)
			if err != nil {
				return opts, fmt.Errorf("invalid RetryPolicy.InitialInterval duration: %w", err)
			}
			retryPolicy.InitialInterval = &durationpb.Duration{Seconds: int64(d.Seconds())}
		}

		if o.RetryPolicy.MaximumInterval != "" {
			d, err := time.ParseDuration(o.RetryPolicy.MaximumInterval)
			if err != nil {
				return opts, fmt.Errorf("invalid RetryPolicy.MaximumInterval duration: %w", err)
			}
			retryPolicy.MaximumInterval = &durationpb.Duration{Seconds: int64(d.Seconds())}
		}

		opts.RetryPolicy = retryPolicy
	}

	return opts, nil
}

func executeActivity(cmd runtime.Command, env bindings.WorkflowEnvironment, dc converter.DataConverter, transcoder payload.Transcoder) error {
	params := cmd.Params()
	if len(params) < 2 {
		return fmt.Errorf("activity command requires at least 2 parameters")
	}

	name, err := extractStringParam(params[0])
	if err != nil {
		return fmt.Errorf("activity name error: %w", err)
	}

	var activityOptions = new(ActivityOptions)
	if err := transcoder.Unmarshal(params[1], activityOptions); err != nil {
		return fmt.Errorf("failed to unmarshal activity options: %w", err)
	}

	tOps, err := activityOptions.ToExecuteActivityOptions()
	if err != nil {
		return fmt.Errorf("failed to convert activity options: %w", err)
	}

	args, err := dc.ToPayloads(params[2:])
	if err != nil {
		return fmt.Errorf("failed to convert activity arguments: %w", err)
	}

	env.ExecuteActivity(bindings.ExecuteActivityParams{
		ExecuteActivityOptions: tOps,
		ActivityType:           struct{ Name string }{Name: name},
		Input:                  args,
	}, func(result *commonpb.Payloads, err error) {
		if err != nil {
			completeCommand(cmd, nil, err)
			return
		}

		var values payload.Payloads
		if err := dc.FromPayloads(result, &values); err != nil {
			completeCommand(cmd, nil, err)
			return
		}

		if len(values) > 0 {
			completeCommand(cmd, values[0], nil)
		} else {
			completeCommand(cmd, nil, nil)
		}
	})

	return nil
}

func executeTimer(cmd runtime.Command, env bindings.WorkflowEnvironment, dc converter.DataConverter, transcoder payload.Transcoder) error {
	params := cmd.Params()
	if len(params) < 1 {
		return fmt.Errorf("timer command requires at least 1 parameter")
	}

	// TimerHeader contains duration in milliseconds
	var timerHeader = new(struct {
		Milliseconds int64 `json:"ms"` // Duration in milliseconds
	})

	if err := transcoder.Unmarshal(params[0], timerHeader); err != nil {
		return fmt.Errorf("failed to unmarshal timer header: %w", err)
	}

	// Convert milliseconds to time.Duration (nanoseconds)
	duration := time.Duration(timerHeader.Milliseconds) * time.Millisecond

	env.NewTimer(duration, workflow.TimerOptions{}, func(result *commonpb.Payloads, err error) {
		if err != nil {
			completeCommand(cmd, nil, err)
			return
		}

		completeCommand(cmd, payload.NewPayload(true, payload.Golang), nil)
	})

	return nil
}

func convertFuncsCallOptions(o *std.FuncsCallOptions) (bindings.ExecuteActivityOptions, error) {
	var opts bindings.ExecuteActivityOptions

	if o.Timeout != "" {
		d, err := time.ParseDuration(o.Timeout)
		if err != nil {
			return opts, fmt.Errorf("invalid Timeout duration: %w", err)
		}
		opts.ScheduleToCloseTimeout = d
	}

	if o.ScheduleToStartTimeout != "" {
		d, err := time.ParseDuration(o.ScheduleToStartTimeout)
		if err != nil {
			return opts, fmt.Errorf("invalid ScheduleToStartTimeout duration: %w", err)
		}
		opts.ScheduleToStartTimeout = d
	}

	if o.StartToCloseTimeout != "" {
		d, err := time.ParseDuration(o.StartToCloseTimeout)
		if err != nil {
			return opts, fmt.Errorf("invalid StartToCloseTimeout duration: %w", err)
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

	opts.TaskQueueName = o.TaskQueue
	opts.WaitForCancellation = o.WaitForCancellation

	if o.Retry != nil {
		retryPolicy := &commonpb.RetryPolicy{
			BackoffCoefficient:     o.Retry.BackoffCoefficient,
			MaximumAttempts:        int32(o.Retry.MaxAttempts),
			NonRetryableErrorTypes: o.Retry.NonRetryableErrors,
		}

		if o.Retry.InitialInterval != "" {
			d, err := time.ParseDuration(o.Retry.InitialInterval)
			if err != nil {
				return opts, fmt.Errorf("invalid Retry.InitialInterval duration: %w", err)
			}
			retryPolicy.InitialInterval = &durationpb.Duration{Seconds: int64(d.Seconds())}
		}

		if o.Retry.MaxInterval != "" {
			d, err := time.ParseDuration(o.Retry.MaxInterval)
			if err != nil {
				return opts, fmt.Errorf("invalid Retry.MaxInterval duration: %w", err)
			}
			retryPolicy.MaximumInterval = &durationpb.Duration{Seconds: int64(d.Seconds())}
		}

		opts.RetryPolicy = retryPolicy
	}

	return opts, nil
}

func executeFuncs(cmd runtime.Command, env bindings.WorkflowEnvironment, dc converter.DataConverter, transcoder payload.Transcoder) error {
	params := cmd.Params()
	if len(params) < 1 {
		return fmt.Errorf("funcs.call command requires at least 1 parameter")
	}

	var header = new(std.FuncsCallHeader)
	if err := transcoder.Unmarshal(params[0], header); err != nil {
		return fmt.Errorf("failed to unmarshal funcs.call header: %w", err)
	}

	var opts bindings.ExecuteActivityOptions
	if header.Options != nil {
		var err error
		opts, err = convertFuncsCallOptions(header.Options)
		if err != nil {
			return fmt.Errorf("failed to convert funcs.call options: %w", err)
		}
	}

	args, err := dc.ToPayloads(params[1:])
	if err != nil {
		return fmt.Errorf("failed to convert function arguments: %w", err)
	}

	env.ExecuteActivity(bindings.ExecuteActivityParams{
		ExecuteActivityOptions: opts,
		ActivityType:           struct{ Name string }{Name: header.Target.String()},
		Input:                  args,
	}, func(result *commonpb.Payloads, err error) {
		if err != nil {
			completeCommand(cmd, nil, err)
			return
		}

		var values payload.Payloads
		if err := dc.FromPayloads(result, &values); err != nil {
			completeCommand(cmd, nil, err)
			return
		}

		if len(values) > 0 {
			completeCommand(cmd, values[0], nil)
		} else {
			completeCommand(cmd, nil, nil)
		}
	})

	return nil
}
