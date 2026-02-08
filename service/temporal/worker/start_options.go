package worker

import (
	"github.com/wippyai/runtime/api/process"
	temporaloptions "github.com/wippyai/runtime/service/temporal/options"
	"go.temporal.io/sdk/client"
)

const (
	optionWorkflowID                               = temporaloptions.OptionWorkflowID
	optionWorkflowTaskQueue                        = temporaloptions.OptionWorkflowTaskQueue
	optionWorkflowExecutionTimeout                 = temporaloptions.OptionWorkflowExecutionTimeout
	optionWorkflowRunTimeout                       = temporaloptions.OptionWorkflowRunTimeout
	optionWorkflowTaskTimeout                      = temporaloptions.OptionWorkflowTaskTimeout
	optionWorkflowIDConflictPolicy                 = temporaloptions.OptionWorkflowIDConflictPolicy
	optionWorkflowIDReusePolicy                    = temporaloptions.OptionWorkflowIDReusePolicy
	optionWorkflowExecutionErrorWhenAlreadyStarted = temporaloptions.OptionWorkflowExecutionErrorWhenAlreadyStarted
	optionWorkflowRetryPolicy                      = temporaloptions.OptionWorkflowRetryPolicy
	optionWorkflowCronSchedule                     = temporaloptions.OptionWorkflowCronSchedule
	optionWorkflowMemo                             = temporaloptions.OptionWorkflowMemo
	optionWorkflowSearchAttributes                 = temporaloptions.OptionWorkflowSearchAttributes
	optionWorkflowTypedSearchAttributes            = temporaloptions.OptionWorkflowTypedSearchAttributes
	optionWorkflowEnableEagerStart                 = temporaloptions.OptionWorkflowEnableEagerStart
	optionWorkflowStartDelay                       = temporaloptions.OptionWorkflowStartDelay
	optionWorkflowStaticSummary                    = temporaloptions.OptionWorkflowStaticSummary
	optionWorkflowStaticDetails                    = temporaloptions.OptionWorkflowStaticDetails
	optionWorkflowVersioningOverride               = temporaloptions.OptionWorkflowVersioningOverride
	optionWorkflowPriority                         = temporaloptions.OptionWorkflowPriority
)

// temporalStartOptionState tracks which conflict-resolution options were explicitly set
// during option application, so callers can decide whether to apply defaults.
type temporalStartOptionState struct {
	hasConflictPolicy bool
	hasErrorOnStarted bool
}

// applyTemporalStartWorkflowOptions maps process.Start options to Temporal StartWorkflowOptions.
func applyTemporalStartWorkflowOptions(opts *client.StartWorkflowOptions, start *process.Start) (temporalStartOptionState, error) {
	var state temporalStartOptionState
	if opts == nil || start == nil {
		return state, nil
	}

	applied, err := temporaloptions.ApplyStartWorkflowOptions(opts, start.Options)
	if err != nil {
		return state, err
	}

	return temporalStartOptionState{
		hasConflictPolicy: applied.HasConflictPolicy,
		hasErrorOnStarted: applied.HasErrorOnStarted,
	}, nil
}
