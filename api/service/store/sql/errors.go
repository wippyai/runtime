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
	ErrDatabaseIDRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "database ID is required",
		retryable: apierror.False,
	}

	ErrTableNameRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "table_name is required",
		retryable: apierror.False,
	}

	ErrIDColumnNameRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "id_column_name is required",
		retryable: apierror.False,
	}

	ErrPayloadColumnNameRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "payload_column_name is required",
		retryable: apierror.False,
	}

	ErrExpireColumnNameRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "expire_column_name is required",
		retryable: apierror.False,
	}

	ErrCleanupIntervalInvalid = &Error{
		kind:      apierror.KindInvalid,
		message:   "cleanup_interval must be greater than or equal to 0",
		retryable: apierror.False,
	}

	ErrDatabaseIDInvalid = &Error{
		kind:      apierror.KindInvalid,
		message:   "database ID is invalid",
		retryable: apierror.False,
	}

	ErrTableNameInvalid = &Error{
		kind:      apierror.KindInvalid,
		message:   "table_name is invalid",
		retryable: apierror.False,
	}

	ErrIDColumnNameInvalid = &Error{
		kind:      apierror.KindInvalid,
		message:   "id_column_name is invalid",
		retryable: apierror.False,
	}

	ErrPayloadColumnNameInvalid = &Error{
		kind:      apierror.KindInvalid,
		message:   "payload_column_name is invalid",
		retryable: apierror.False,
	}

	ErrExpireColumnNameInvalid = &Error{
		kind:      apierror.KindInvalid,
		message:   "expire_column_name is invalid",
		retryable: apierror.False,
	}
)

func NewInvalidCleanupIntervalError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid CleanupInterval duration format",
		retryable: apierror.False,
		cause:     cause,
	}
}
