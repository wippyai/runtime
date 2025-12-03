package component

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Error implements apierror.Error for component manager errors
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

// NewLoadWASMError creates an error for WASM loading from filesystem failure
func NewLoadWASMError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "load WASM from fs: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewHashVerificationError creates an error for hash verification failure
func NewHashVerificationError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "hash verification failed: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewLoadComponentError creates an error for component loading failure
func NewLoadComponentError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "load component: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewCompileError creates an error for component compilation failure
func NewCompileError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "compile component: " + err.Error(),
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

// NewComponentNotFoundError creates an error when component is not found
func NewComponentNotFoundError(id registry.ID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "component " + id.String() + " not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"component_id": id.String()}),
	}
}

// NewFilesystemNotFoundError creates an error when filesystem is not found
func NewFilesystemNotFoundError(fsID string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "filesystem " + fsID + " not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"fs_id": fsID}),
	}
}

// NewOpenFileError creates an error for file opening failure
func NewOpenFileError(path string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "open " + path + ": " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"path": path, "cause": err.Error()}),
		cause:     err,
	}
}

// NewInvalidHashFormatError creates an error for invalid hash format
func NewInvalidHashFormatError(hash string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid hash format, expected 'algorithm:hash', got \"" + hash + "\"",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"hash": hash}),
	}
}

// NewUnsupportedHashAlgorithmError creates an error for unsupported hash algorithm
func NewUnsupportedHashAlgorithmError(algorithm string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported hash algorithm: " + algorithm,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"algorithm": algorithm}),
	}
}

// NewHashMismatchError creates an error for hash mismatch
func NewHashMismatchError(expected, actual string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "hash mismatch: expected " + expected + ", got " + actual,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"expected": expected, "actual": actual}),
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
