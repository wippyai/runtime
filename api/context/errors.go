package context

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Errors returned by context operations.
var (
	ErrNoFrameContext = &Error{
		kind:      apierror.KindInvalid,
		message:   "no frame context available",
		retryable: apierror.False,
	}

	ErrNoAppContext = &Error{
		kind:      apierror.KindInvalid,
		message:   "no app context available",
		retryable: apierror.False,
	}

	ErrFrameSealed = &Error{
		kind:      apierror.KindInvalid,
		message:   "frame is sealed",
		retryable: apierror.False,
	}
)

// Error represents a context error with additional metadata.
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

// NewFrameSealedError creates an error for attempting to set a key in a sealed frame.
func NewFrameSealedError(key any) *Error {
	details := attrs.NewBag()
	details.Set("key", key)
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "cannot set key in sealed frame",
		retryable: apierror.False,
		details:   details,
	}
}
