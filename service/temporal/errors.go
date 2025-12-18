package temporal

import (
	"errors"

	apierror "github.com/wippyai/runtime/api/error"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
)

// ToApplicationError converts any error to a Temporal ApplicationError.
// Preserves full error chain with stack traces in details.
func ToApplicationError(err error) error {
	if err == nil {
		return nil
	}

	chain := apierror.BuildChain(err)
	if chain == nil {
		return temporal.NewApplicationError(err.Error(), string(apierror.Internal))
	}

	root := chain.Root()
	if root == nil {
		return temporal.NewApplicationError(err.Error(), string(apierror.Internal))
	}

	errType := root.Kind
	if errType == "" {
		errType = string(apierror.Internal)
	}

	nonRetryable := false
	if root.Retryable != nil {
		nonRetryable = !*root.Retryable
	}

	opts := temporal.ApplicationErrorOptions{
		NonRetryable: nonRetryable,
		Cause:        err,
		Details:      []any{*chain},
	}

	return temporal.NewApplicationErrorWithOptions(root.Message, errType, opts)
}

// FromTemporalError converts a Temporal error to apierror.Rich.
// Reconstructs the error chain from serialized details.
func FromTemporalError(err error) apierror.Rich {
	if err == nil {
		return nil
	}

	// Unwrap wrapper errors to get to the actual error
	var activityErr *temporal.ActivityError
	var childErr *temporal.ChildWorkflowExecutionError

	if errors.As(err, &activityErr) {
		rich := fromTemporalErrorInner(errors.Unwrap(activityErr))
		if rich != nil {
			addWrapperDetails(rich, map[string]any{
				"_wrapper":     "activity",
				"_activity_id": activityErr.ActivityID(),
				"_retry_state": activityErr.RetryState().String(),
			})
		}
		return rich
	}

	if errors.As(err, &childErr) {
		rich := fromTemporalErrorInner(errors.Unwrap(childErr))
		if rich != nil {
			addWrapperDetails(rich, map[string]any{
				"_wrapper":       "child_workflow",
				"_workflow_id":   childErr.WorkflowID(),
				"_workflow_type": childErr.WorkflowType(),
				"_run_id":        childErr.RunID(),
				"_retry_state":   childErr.RetryState().String(),
			})
		}
		return rich
	}

	return fromTemporalErrorInner(err)
}

func addWrapperDetails(rich apierror.Rich, wrapper map[string]any) {
	if re, ok := rich.(*apierror.RichError); ok {
		if re.Details() == nil {
			re.WithDetails(wrapper)
		} else {
			for k, v := range wrapper {
				re.Details()[k] = v
			}
		}
	}
}

func fromTemporalErrorInner(err error) apierror.Rich {
	if err == nil {
		return nil
	}

	// ApplicationError with our chain in details
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		var chain apierror.Chain
		if appErr.Details(&chain) == nil && len(chain.Errors) > 0 {
			return apierror.FromChain(&chain)
		}

		// No chain in details - create from ApplicationError fields
		e := apierror.NewRich(mapTypeToKind(appErr.Type()), appErr.Message())
		if appErr.NonRetryable() {
			e.WithRetryable(apierror.False)
		} else {
			e.WithRetryable(apierror.True)
		}
		return e
	}

	// CanceledError
	var canceledErr *temporal.CanceledError
	if errors.As(err, &canceledErr) {
		return apierror.NewRich(apierror.Canceled, "operation canceled").
			WithRetryable(apierror.False)
	}

	// TimeoutError
	var timeoutErr *temporal.TimeoutError
	if errors.As(err, &timeoutErr) {
		return apierror.NewRich(apierror.Timeout, timeoutErr.Message()).
			WithRetryable(apierror.False).
			WithDetails(map[string]any{
				"timeout_type": timeoutTypeToString(timeoutErr.TimeoutType()),
			})
	}

	// PanicError
	var panicErr *temporal.PanicError
	if errors.As(err, &panicErr) {
		return apierror.NewRich(apierror.Internal, panicErr.Error()).
			WithRetryable(apierror.False).
			WithStack([]string{panicErr.StackTrace()})
	}

	// TerminatedError
	var terminatedErr *temporal.TerminatedError
	if errors.As(err, &terminatedErr) {
		return apierror.NewRich(apierror.Canceled, "workflow terminated").
			WithRetryable(apierror.False)
	}

	// Unknown error type - wrap as Internal
	return apierror.NewRich(apierror.Internal, err.Error())
}

// mapTypeToKind converts Temporal error type string to apierror.Kind.
func mapTypeToKind(errType string) apierror.Kind {
	switch errType {
	case "NotFound":
		return apierror.NotFound
	case "AlreadyExists":
		return apierror.AlreadyExists
	case "Invalid":
		return apierror.Invalid
	case "PermissionDenied":
		return apierror.PermissionDenied
	case "Unavailable":
		return apierror.Unavailable
	case "Internal":
		return apierror.Internal
	case "Canceled":
		return apierror.Canceled
	case "Conflict":
		return apierror.Conflict
	case "Timeout":
		return apierror.Timeout
	case "RateLimited":
		return apierror.RateLimited
	default:
		return apierror.Unknown
	}
}

// timeoutTypeToString converts Temporal timeout type to string.
func timeoutTypeToString(tt enumspb.TimeoutType) string {
	switch tt {
	case enumspb.TIMEOUT_TYPE_START_TO_CLOSE:
		return "start_to_close"
	case enumspb.TIMEOUT_TYPE_SCHEDULE_TO_START:
		return "schedule_to_start"
	case enumspb.TIMEOUT_TYPE_SCHEDULE_TO_CLOSE:
		return "schedule_to_close"
	case enumspb.TIMEOUT_TYPE_HEARTBEAT:
		return "heartbeat"
	default:
		return "unknown"
	}
}
