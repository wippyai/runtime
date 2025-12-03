package workflow

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
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

func NewInvalidEntryKindError(got, expected string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid entry kind " + got + ", expected " + expected,
		retryable: apierror.False,
	}
}

func NewUnpackConfigError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to unpack workflow config",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewAddWorkflowNodeError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to add workflow node",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewRegisterFactoryError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to register factory",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewUpdateWorkflowNodeError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to update workflow node",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewUpdateFactoryError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to update factory",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewDeleteWorkflowNodeError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to delete workflow node",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewCompileError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to compile",
		retryable: apierror.False,
		cause:     cause,
	}
}
