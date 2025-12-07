package supervisor

import (
	"time"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error kind constants specific to supervisor.
const (
	KindTerminated apierror.Kind = "Terminated"
	KindExited     apierror.Kind = "Exited"
)

// Errors returned by supervisor operations.
var (
	ErrTerminated = &Error{
		kind:      KindTerminated,
		message:   "service terminated",
		retryable: apierror.False,
	}

	ErrExit = &Error{
		kind:      KindExited,
		message:   "service exited",
		retryable: apierror.False,
	}

	ErrStartTimeout = &Error{
		kind:      apierror.KindTimeout,
		message:   "service start timed out",
		retryable: apierror.True,
	}

	ErrOutsideTransaction = &Error{
		kind:      apierror.KindInvalid,
		message:   "action received outside of transaction",
		retryable: apierror.False,
	}
)

// Error represents a supervisor error with metadata.
type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

// WithCause returns a new error with the given cause.
func (e *Error) WithCause(cause error) *Error {
	return &Error{
		kind:      e.kind,
		message:   e.message,
		retryable: e.retryable,
		details:   e.details,
		cause:     cause,
	}
}

// WithMessage returns a new error with a custom message.
func (e *Error) WithMessage(msg string) *Error {
	return &Error{
		kind:      e.kind,
		message:   msg,
		retryable: e.retryable,
		details:   e.details,
		cause:     e.cause,
	}
}

// NewInvalidDurationError creates an error for invalid duration format.
func NewInvalidDurationError(field string, cause error) *Error {
	details := attrs.NewBag()
	details.Set("field", field)
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid " + field + " duration format",
		retryable: apierror.False,
		details:   details,
		cause:     cause,
	}
}

// NewServiceNotFoundError creates an error when service is not found.
func NewServiceNotFoundError(serviceID string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "service " + serviceID + " not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"service_id": serviceID}),
	}
}

// NewSubscriberError creates an error for event subscriber failures.
func NewSubscriberError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create event subscriber: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewDependencyResolveError creates an error when dependency resolution fails.
func NewDependencyResolveError(serviceID string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to resolve dependencies for " + serviceID + ": " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()}),
		cause:     err,
	}
}

// NewStartOperationsError creates an error when building start operations fails.
func NewStartOperationsError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to build start operations: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewTransitionError creates an error when state transitions fail.
func NewTransitionError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to execute transitions: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewStopError creates an error when stopping a service fails.
func NewStopError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to stop service: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewSupervisorStoppedError creates an error when supervisor is stopped.
func NewSupervisorStoppedError(err error) *Error {
	return &Error{
		kind:      apierror.KindUnavailable,
		message:   "supervisor is stopped: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewStopTimeoutError creates an error when service stop times out.
func NewStopTimeoutError(timeout time.Duration) *Error {
	return &Error{
		kind:      apierror.KindTimeout,
		message:   "service stop timed out after " + timeout.String(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"timeout": timeout.String()}),
	}
}

// NewServiceStartError creates an error when a service fails to start.
func NewServiceStartError(serviceID string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to start service " + serviceID + ": " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()}),
		cause:     err,
	}
}

// NewServiceStopError creates an error when a service fails to stop.
func NewServiceStopError(serviceID string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to stop service " + serviceID + ": " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()}),
		cause:     err,
	}
}

// NewStartSequenceError creates an error when start sequence fails.
func NewStartSequenceError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "start sequence failed: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewStopSequenceError creates an error when stop sequence fails.
func NewStopSequenceError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "stop sequence failed: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewDependencyLevelsError creates an error when determining dependency levels fails.
func NewDependencyLevelsError(phase string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to determine " + phase + " dependency levels: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"phase": phase, "cause": err.Error()}),
		cause:     err,
	}
}

// NewMultiStartError creates an error when multiple service starts fail.
func NewMultiStartError(count int, firstErr error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "start failed for multiple services: " + firstErr.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"failed_count": count, "first_error": firstErr.Error()}),
		cause:     firstErr,
	}
}

// NewMultiStopError creates an error when multiple service stops fail.
func NewMultiStopError(count int, firstErr error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "stop failed for multiple services: " + firstErr.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"failed_count": count, "first_error": firstErr.Error()}),
		cause:     firstErr,
	}
}

// NewCommitRemoveError creates an error when removing service during commit fails.
func NewCommitRemoveError(serviceID string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to remove service " + serviceID + " during commit: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()}),
		cause:     err,
	}
}

// NewCommitRegisterError creates an error when registering service during commit fails.
func NewCommitRegisterError(serviceID string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to register service " + serviceID + " during commit: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"service_id": serviceID, "cause": err.Error()}),
		cause:     err,
	}
}
