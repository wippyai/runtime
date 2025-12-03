package engine

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error implements apierror.Error for engine errors
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

// NewCompileWATError creates an error for WAT compilation failure
func NewCompileWATError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "compile WAT: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewAsyncifyTransformError creates an error for asyncify transformation failure
func NewAsyncifyTransformError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "asyncify transform: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewLoadWASMError creates an error for WASM loading failure
func NewLoadWASMError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "load WASM: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewInstantiateModuleError creates an error for module instantiation failure
func NewInstantiateModuleError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "instantiate module: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewTranscodePayloadError creates an error for payload transcoding failure
func NewTranscodePayloadError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "transcode payload: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewFunctionNotFoundError creates an error when a function is not found
func NewFunctionNotFoundError(method string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "function \"" + method + "\" not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"method": method}),
	}
}

// NewFunctionTypeError creates an error when a function has unexpected type
func NewFunctionTypeError(method string) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "function \"" + method + "\" has unexpected type",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"method": method}),
	}
}

// NewSchedulerStateError creates an error for invalid scheduler state
func NewSchedulerStateError(msg string) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "scheduler: " + msg,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"state_error": msg}),
	}
}

// NewSchedulerRewindError creates an error for rewind operation failure
func NewSchedulerRewindError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "scheduler: start rewind: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewSchedulerUnwindError creates an error for unwind operation failure
func NewSchedulerUnwindError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "scheduler: stop unwind: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewTransportPrepareError creates an error for transport preparation failure
func NewTransportPrepareError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "transport prepare: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// Sentinel errors

// ErrExternalMessagesNotSupported is returned when sending external messages to WASM processes
var ErrExternalMessagesNotSupported = &Error{
	kind:      apierror.KindInvalid,
	message:   "WASM processes do not support external messages",
	retryable: apierror.False,
}
