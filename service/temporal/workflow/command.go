package workflow

import (
	"fmt"
	"log"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
	lua "github.com/yuin/gopher-lua"

	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	bindings "go.temporal.io/sdk/internalbindings"
	workflow "go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/types/known/durationpb"
)

//------------------------------------------------------------------------------
// COMMAND SYSTEM - Constants, Types, and Registry
//------------------------------------------------------------------------------

// Command type constants
const (
	ActivityCommandType = "activity"
	TimerCommandType    = "timer"
)

// CommandHandlerFunc defines the signature for command handler functions
type CommandHandlerFunc func(cmd runtime.Command, env bindings.WorkflowEnvironment, dc converter.DataConverter, transcoder payload.Transcoder) error

// commandHandlers registry stores all registered command handlers
var commandHandlers = map[string]CommandHandlerFunc{
	ActivityCommandType: executeActivity,
	TimerCommandType:    executeTimer,
}

// RegisterCommandHandler registers a new command type and its handler
func RegisterCommandHandler(commandType string, handler CommandHandlerFunc) {
	commandHandlers[commandType] = handler
}

// ExecuteCommand executes a command based on its type
func ExecuteCommand(cmd runtime.Command, env bindings.WorkflowEnvironment, dc converter.DataConverter, transcoder payload.Transcoder) error {
	handler, exists := commandHandlers[cmd.Type()]
	if !exists {
		return fmt.Errorf("no handler registered for command type: %s", cmd.Type())
	}

	return handler(cmd, env, dc, transcoder)
}

//------------------------------------------------------------------------------
// Helper Functions
//------------------------------------------------------------------------------

// completeCommand is a helper function to complete a command with result or error
func completeCommand(cmd runtime.Command, value payload.Payload, err error) {
	var result *runtime.Result
	if err != nil {
		result = &runtime.Result{Error: err}
	} else {
		result = &runtime.Result{Value: value}
	}

	if completeErr := cmd.Complete(result); completeErr != nil {
		log.Printf("Failed to complete command %s: %v", cmd.ID(), completeErr)
	}
}

// extractStringParam extracts a string parameter from a payload
func extractStringParam(param payload.Payload) (string, error) {
	if str, ok := param.Data().(string); ok {
		return str, nil
	}

	if lv, ok := param.Data().(lua.LValue); ok {
		return lv.String(), nil
	}

	return "", fmt.Errorf("parameter must be a string")
}

//------------------------------------------------------------------------------
// Options Structures
//------------------------------------------------------------------------------

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
func (o *ActivityOptions) ToExecuteActivityOptions() (bindings.ExecuteActivityOptions, error) {
	var opts bindings.ExecuteActivityOptions

	// Convert duration strings to time.Duration
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

//------------------------------------------------------------------------------
// COMMAND HANDLERS - Add new handlers below
//------------------------------------------------------------------------------

// executeActivity handles the execution of an activity command
func executeActivity(cmd runtime.Command, env bindings.WorkflowEnvironment, dc converter.DataConverter, transcoder payload.Transcoder) error {
	params := cmd.Params()
	if len(params) < 2 {
		return fmt.Errorf("activity command requires at least 2 parameters")
	}

	// Extract activity name
	name, err := extractStringParam(params[0])
	if err != nil {
		return fmt.Errorf("activity name error: %w", err)
	}

	// Extract activity options
	var activityOptions = new(ActivityOptions)
	if err := transcoder.Unmarshal(params[1], activityOptions); err != nil {
		return fmt.Errorf("failed to unmarshal activity options: %w", err)
	}

	// Convert options to Temporal format
	tOps, err := activityOptions.ToExecuteActivityOptions()
	if err != nil {
		return fmt.Errorf("failed to convert activity options: %w", err)
	}

	// Prepare activity arguments
	args, err := dc.ToPayloads(params[2:])
	if err != nil {
		return fmt.Errorf("failed to convert activity arguments: %w", err)
	}

	// Execute the activity
	env.ExecuteActivity(bindings.ExecuteActivityParams{
		ExecuteActivityOptions: tOps,
		ActivityType:           struct{ Name string }{Name: name},
		Input:                  args,
	}, func(result *commonpb.Payloads, err error) {
		if err != nil {
			completeCommand(cmd, nil, err)
			return
		}

		// Convert result for the command
		var values payload.Payloads
		if err := dc.FromPayloads(result, &values); err != nil {
			completeCommand(cmd, nil, err)
			return
		}

		// Complete with the first result or nil
		if len(values) > 0 {
			completeCommand(cmd, values[0], nil)
		} else {
			completeCommand(cmd, nil, nil)
		}
	})

	return nil
}

// executeTimer handles the execution of a timer command
func executeTimer(cmd runtime.Command, env bindings.WorkflowEnvironment, dc converter.DataConverter, transcoder payload.Transcoder) error {
	params := cmd.Params()
	if len(params) < 1 {
		return fmt.Errorf("timer command requires at least 1 parameter")
	}

	// Extract timer options
	var timerOptions = new(struct {
		Duration string `json:"duration"`
	})

	if err := transcoder.Unmarshal(params[0], timerOptions); err != nil {
		return fmt.Errorf("failed to unmarshal timer options: %w", err)
	}

	// Parse duration
	duration, err := time.ParseDuration(timerOptions.Duration)
	if err != nil {
		return fmt.Errorf("failed to parse duration '%s': %w", timerOptions.Duration, err)
	}

	// Execute the timer
	env.NewTimer(duration, workflow.TimerOptions{}, func(result *commonpb.Payloads, err error) {
		if err != nil {
			completeCommand(cmd, nil, err)
			return
		}

		// Complete with success
		completeCommand(cmd, payload.NewPayload(true, payload.Golang), nil)
	})

	return nil
}
