package temporal

import (
	"errors"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"go.temporal.io/sdk/temporal"
)

// ToApplicationError converts an apierror.Error to a Temporal ApplicationError.
// This preserves the error kind as type, retryability, and details.
func ToApplicationError(err error) error {
	if err == nil {
		return nil
	}

	var apiErr apierror.Error
	if !errors.As(err, &apiErr) {
		// Plain error - wrap as non-retryable Internal error
		return temporal.NewNonRetryableApplicationError(
			err.Error(),
			string(apierror.Internal),
			err,
		)
	}

	errType := string(apiErr.Kind())
	message := apiErr.Error()

	// Convert retryability
	if apiErr.Retryable() == apierror.False {
		return temporal.NewNonRetryableApplicationError(message, errType, err)
	}

	// Retryable or unspecified - let Temporal handle retry decisions
	return temporal.NewApplicationError(message, errType, err)
}

// FromTemporalError converts a Temporal error to an apierror.Error.
// This extracts kind from ApplicationError type, handles cancellation/timeout.
func FromTemporalError(err error) apierror.Error {
	if err == nil {
		return nil
	}

	// Check for ApplicationError
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		kind := mapTypeToKind(appErr.Type())
		retryable := apierror.True
		if appErr.NonRetryable() {
			retryable = apierror.False
		}
		return apierror.E(kind, appErr.Message(), retryable, nil, err)
	}

	// Check for CanceledError
	var canceledErr *temporal.CanceledError
	if errors.As(err, &canceledErr) {
		return apierror.New(apierror.Canceled, "operation canceled").WithRetryable(apierror.False).WithCause(err)
	}

	// Check for TimeoutError
	var timeoutErr *temporal.TimeoutError
	if errors.As(err, &timeoutErr) {
		return apierror.New(apierror.Timeout, err.Error()).WithRetryable(apierror.False).WithCause(err)
	}

	// Check for PanicError
	var panicErr *temporal.PanicError
	if errors.As(err, &panicErr) {
		details := attrs.NewBagFrom(map[string]any{
			"stack_trace": panicErr.StackTrace(),
		})
		return apierror.New(apierror.Internal, panicErr.Error()).
			WithRetryable(apierror.False).
			WithDetails(details).
			WithCause(err)
	}

	// Unknown error type - wrap as Internal
	return apierror.New(apierror.Internal, err.Error()).WithRetryable(apierror.Unspecified).WithCause(err)
}

// mapTypeToKind converts Temporal error type string to apierror.Kind.
func mapTypeToKind(errType string) apierror.Kind {
	switch apierror.Kind(errType) {
	case apierror.NotFound:
		return apierror.NotFound
	case apierror.AlreadyExists:
		return apierror.AlreadyExists
	case apierror.Invalid:
		return apierror.Invalid
	case apierror.PermissionDenied:
		return apierror.PermissionDenied
	case apierror.Unavailable:
		return apierror.Unavailable
	case apierror.Internal:
		return apierror.Internal
	case apierror.Canceled:
		return apierror.Canceled
	case apierror.Conflict:
		return apierror.Conflict
	case apierror.Timeout:
		return apierror.Timeout
	case apierror.RateLimited:
		return apierror.RateLimited
	default:
		return apierror.Unknown
	}
}

func NewInvalidConnectionTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid connection timeout duration").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidKeepAliveTimeError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid keep alive time duration").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidKeepAliveTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid keep alive timeout duration").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidHealthCheckIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid health check interval duration").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidAuthTypeError(authType string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid auth type: "+authType).WithRetryable(apierror.False)
}

func NewInvalidStickyScheduleToStartTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid sticky schedule to start timeout").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidWorkerStopTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid worker stop timeout").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidDeadlockDetectionTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid deadlock detection timeout").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidMaxHeartbeatThrottleIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid max heartbeat throttle interval").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidDefaultHeartbeatThrottleIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid default heartbeat throttle interval").WithCause(cause).WithRetryable(apierror.False)
}
