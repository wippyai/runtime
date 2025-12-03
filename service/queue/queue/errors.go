package queue

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

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

func newConfigDecodeError(cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode queue config",
		retryable: apierror.False,
		cause:     cause,
	}
}

func newConfigValidationError(cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid queue config",
		retryable: apierror.False,
		cause:     cause,
	}
}

func newDriverNotFoundError(driverID registry.ID) error {
	details := attrs.NewBag()
	details.Set("driver_id", driverID.String())
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "driver not found: " + driverID.String(),
		retryable: apierror.False,
		details:   details,
	}
}
