package resource

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors for resource operations.
var (
	ErrNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "resource not found",
		retryable: apierror.False,
	}

	ErrLocked = &Error{
		kind:      apierror.KindUnavailable,
		message:   "resource is locked",
		retryable: apierror.True,
	}

	ErrReleased = &Error{
		kind:      apierror.KindInvalid,
		message:   "resource has been released",
		retryable: apierror.False,
	}

	ErrClosed = &Error{
		kind:      apierror.KindUnavailable,
		message:   "resource provider is closed",
		retryable: apierror.False,
	}

	ErrInUse = &Error{
		kind:      apierror.KindUnavailable,
		message:   "resource is in use",
		retryable: apierror.True,
	}
)

// Error implements apierror.Error for resource errors.
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

// Is implements error matching for sentinel errors.
func (e *Error) Is(target error) bool {
	if t, ok := target.(*Error); ok {
		return e.kind == t.kind && e.message == t.message
	}
	return false
}

// NewSubscriberError creates an error for subscriber creation failure.
func NewSubscriberError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create subscriber: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}
