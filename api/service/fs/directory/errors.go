package directory

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// ErrEmptyDirectoryPath is returned when the directory path is empty.
var ErrEmptyDirectoryPath = &Error{
	kind:      apierror.KindInvalid,
	message:   "directory path cannot be empty",
	retryable: apierror.False,
}

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

func NewInvalidModeFormatError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid mode format",
		retryable: apierror.False,
		cause:     cause,
	}
}
