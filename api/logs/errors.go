package logs

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error implements apierror.Error for logs errors
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

// Sentinel errors
var (
	ErrGetConfigTimeout        = &Error{kind: apierror.KindTimeout, message: "timeout waiting for log config", retryable: apierror.True}
	ErrSetConfigTimeout        = &Error{kind: apierror.KindTimeout, message: "timeout waiting for config confirmation", retryable: apierror.True}
	ErrGetLoggingConfigTimeout = &Error{kind: apierror.KindTimeout, message: "failed to get logging config", retryable: apierror.True}
	ErrSetTempConfigTimeout    = &Error{kind: apierror.KindTimeout, message: "failed to set temporary config", retryable: apierror.True}
)

// NewSubscriberError creates an error when creating a subscriber fails
func NewSubscriberError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create subscriber: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewContextCanceledError creates an error when context is canceled
func NewContextCanceledError(err error) *Error {
	return &Error{
		kind:      apierror.KindCanceled,
		message:   "context canceled: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewConfigMismatchError creates an error when config confirmation doesn't match
func NewConfigMismatchError(requested, got string) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "config mismatch - requested: " + requested + ", got: " + got,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"requested": requested, "got": got}),
	}
}

// NewGetLoggingConfigError creates an error when getting logging config fails
func NewGetLoggingConfigError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to get logging config: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewSetTempConfigError creates an error when setting temporary config fails
func NewSetTempConfigError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to set temporary config: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}
