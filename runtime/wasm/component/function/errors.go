package function

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Error implements apierror.Error for function manager errors
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

// NewRuntimeError creates an error for WASM runtime creation failure
func NewRuntimeError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "create WASM runtime: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewRegisterHostError creates an error for host registration failure
func NewRegisterHostError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "register clock host: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewInvalidEntryKindError creates an error for invalid entry kind
func NewInvalidEntryKindError(actual, expected string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid entry kind " + actual + ", expected " + expected,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"actual": actual, "expected": expected}),
	}
}

// NewUnpackConfigError creates an error for config unpacking failure
func NewUnpackConfigError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unpack config: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

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

// NewCreatePoolError creates an error for pool creation failure
func NewCreatePoolError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "create pool: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewReplacePoolError creates an error for pool replacement failure
func NewReplacePoolError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "replace pool: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewFunctionNotFoundError creates an error when function is not found
func NewFunctionNotFoundError(id registry.ID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "function " + id.String() + " not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"function_id": id.String()}),
	}
}

// NewUnknownPoolTypeError creates an error for unknown pool type
func NewUnknownPoolTypeError(poolType string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unknown pool type: " + poolType,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"pool_type": poolType}),
	}
}

// NewTranscoderNotFoundError creates an error when transcoder is not in context
func NewTranscoderNotFoundError() *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "transcoder not found in context",
		retryable: apierror.False,
	}
}

// NewUnmarshalConfigError creates an error for config unmarshaling failure
func NewUnmarshalConfigError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to unmarshal config: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// Sentinel errors

// ErrManagerNotStarted is returned when manager operations are called before Start
var ErrManagerNotStarted = &Error{
	kind:      apierror.KindUnavailable,
	message:   "manager not started",
	retryable: apierror.False,
}
