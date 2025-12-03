package memory

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

var (
	ErrInvalidMaxSize = &Error{
		kind:      apierror.KindInvalid,
		message:   "max_size must be greater than or equal to 0",
		retryable: apierror.False,
	}

	ErrInvalidCleanupInterval = &Error{
		kind:      apierror.KindInvalid,
		message:   "cleanup_interval must be greater than or equal to 0",
		retryable: apierror.False,
	}
)

func NewInvalidDurationError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid CleanupInterval duration format",
		retryable: apierror.False,
		cause:     cause,
	}
}
