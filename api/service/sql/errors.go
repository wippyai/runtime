package sql

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
	ErrHostRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "host is required",
		retryable: apierror.False,
	}

	ErrInvalidPort = &Error{
		kind:      apierror.KindInvalid,
		message:   "port must be greater than 0",
		retryable: apierror.False,
	}

	ErrDatabaseRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "database is required",
		retryable: apierror.False,
	}

	ErrUsernameRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "username is required",
		retryable: apierror.False,
	}

	ErrPasswordRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "password is required",
		retryable: apierror.False,
	}

	ErrInvalidMaxOpen = &Error{
		kind:      apierror.KindInvalid,
		message:   "pool.max_open must be greater or equal to 0",
		retryable: apierror.False,
	}

	ErrInvalidMaxIdle = &Error{
		kind:      apierror.KindInvalid,
		message:   "pool.max_idle must be greater than or equal to 0",
		retryable: apierror.False,
	}

	ErrInvalidMaxLifetime = &Error{
		kind:      apierror.KindInvalid,
		message:   "pool.max_lifetime must be greater than 0",
		retryable: apierror.False,
	}

	ErrFileRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "file is required",
		retryable: apierror.False,
	}
)

func NewInvalidDurationError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid max_lifetime duration format",
		retryable: apierror.False,
		cause:     cause,
	}
}
