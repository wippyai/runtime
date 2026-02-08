package workflow

import (
	"github.com/wippyai/runtime/api/attrs"
	temporaloptions "github.com/wippyai/runtime/service/temporal/options"
	bindings "go.temporal.io/sdk/internalbindings"
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
	optionWorkflowTypedSearchAttrs                 = temporaloptions.OptionWorkflowTypedSearchAttributes
	optionWorkflowEnableEagerStart                 = temporaloptions.OptionWorkflowEnableEagerStart
	optionWorkflowStartDelay                       = temporaloptions.OptionWorkflowStartDelay
	optionWorkflowStaticSummary                    = temporaloptions.OptionWorkflowStaticSummary
	optionWorkflowStaticDetails                    = temporaloptions.OptionWorkflowStaticDetails
	optionWorkflowVersioningOverride               = temporaloptions.OptionWorkflowVersioningOverride
	optionWorkflowPriority                         = temporaloptions.OptionWorkflowPriority
	optionWorkflowNamespace                        = temporaloptions.OptionWorkflowNamespace
	optionWorkflowWaitForCancellation              = temporaloptions.OptionWorkflowWaitForCancellation
	optionWorkflowParentClosePolicy                = temporaloptions.OptionWorkflowParentClosePolicy
	optionWorkflowVersioningIntent                 = temporaloptions.OptionWorkflowVersioningIntent
)

// applyTemporalActivityOptions maps attribute options to Temporal activity execution options.
func applyTemporalActivityOptions(opts *bindings.ExecuteActivityOptions, options attrs.Attributes) error {
	return temporaloptions.ApplyActivityOptions(opts, options)
}

// applyTemporalChildWorkflowOptions maps attribute options to Temporal child workflow params.
func applyTemporalChildWorkflowOptions(params *bindings.ExecuteWorkflowParams, options attrs.Attributes) error {
	return temporaloptions.ApplyChildWorkflowOptions(params, options)
}
