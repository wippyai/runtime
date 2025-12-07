package fs

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

func (e *Error) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

var ErrFileAlreadyClosed = &Error{
	kind:      apierror.KindInvalid,
	message:   "file already closed",
	retryable: apierror.False,
}

func NewReadError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to read",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewWriteError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to write",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewSeekError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to seek",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewStatError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to stat",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewSyncError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to sync",
		retryable: apierror.False,
		cause:     cause,
	}
}
