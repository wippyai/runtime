package http

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
	ErrEmptyAddr = &Error{
		kind:      apierror.KindInvalid,
		message:   "server address cannot be empty",
		retryable: apierror.False,
	}

	ErrNegativeTimeout = &Error{
		kind:      apierror.KindInvalid,
		message:   "timeout must be positive or zero (default)",
		retryable: apierror.False,
	}

	ErrNilMetadata = &Error{
		kind:      apierror.KindInvalid,
		message:   "metadata cannot be nil",
		retryable: apierror.False,
	}

	ErrEmptyFuncName = &Error{
		kind:      apierror.KindInvalid,
		message:   "func name cannot be empty",
		retryable: apierror.False,
	}

	ErrEmptyPath = &Error{
		kind:      apierror.KindInvalid,
		message:   "endpoint path cannot be empty",
		retryable: apierror.False,
	}

	ErrEmptyMethod = &Error{
		kind:      apierror.KindInvalid,
		message:   "endpoint method cannot be empty",
		retryable: apierror.False,
	}
)

func NewInvalidDurationError(field string, cause error) *Error {
	details := attrs.NewBag()
	details.Set("field", field)
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid " + field + " duration format",
		retryable: apierror.False,
		details:   details,
		cause:     cause,
	}
}

func NewInvalidTimeoutError(field string) *Error {
	details := attrs.NewBag()
	details.Set("field", field)
	return &Error{
		kind:      apierror.KindInvalid,
		message:   field + " must be positive or zero (default)",
		retryable: apierror.False,
		details:   details,
	}
}

func NewPathMustStartWithSlashError() *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "endpoint path must start with /",
		retryable: apierror.False,
	}
}

func NewInvalidHTTPMethodError(method string) *Error {
	details := attrs.NewBag()
	details.Set("method", method)
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid HTTP method: " + method,
		retryable: apierror.False,
		details:   details,
	}
}

func NewMissingMetadataError(key string) *Error {
	details := attrs.NewBag()
	details.Set("key", key)
	return &Error{
		kind:      apierror.KindInvalid,
		message:   key + " in metadata cannot be empty",
		retryable: apierror.False,
		details:   details,
	}
}

func NewNegativeConfigError(field string) *Error {
	details := attrs.NewBag()
	details.Set("field", field)
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "host " + field + " must be positive or zero (default)",
		retryable: apierror.False,
		details:   details,
	}
}

func NewInvalidTimeoutConfigError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid timeout configuration",
		retryable: apierror.False,
		cause:     cause,
	}
}
