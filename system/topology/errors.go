package topology

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/relay"
)

// Error implements apierror.Error for topology errors
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

// NewUnregisteredPIDError creates an error for unregistered PID operations
func NewUnregisteredPIDError(operation string, pid relay.PID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "cannot " + operation + " unregistered pid: " + pid.String(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"operation": operation, "pid": pid.String()}),
	}
}

// NewAlreadyMonitoringError creates an error when already monitoring a PID
func NewAlreadyMonitoringError(pid relay.PID) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "already monitoring pid: " + pid.String(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"pid": pid.String()}),
	}
}
