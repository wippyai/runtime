package engine

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

// NewInstantiateError creates an error for module instantiation failures.
func NewInstantiateError(err error) error {
	return apierror.New(apierror.Internal, "failed to instantiate WASM module").
		WithCause(err).
		WithRetryable(apierror.False)
}

// NewFunctionNotFoundError creates an error for missing exported functions.
func NewFunctionNotFoundError(name string) error {
	return apierror.New(apierror.NotFound, fmt.Sprintf("function %q not found", name)).
		WithRetryable(apierror.False)
}

// NewFunctionTypeError creates an error for invalid function types.
func NewFunctionTypeError(name string) error {
	return apierror.New(apierror.Internal, fmt.Sprintf("function %q has invalid type", name)).
		WithRetryable(apierror.False)
}

// NewTransportPrepareError creates an error for transport preparation failures.
func NewTransportPrepareError(err error) error {
	return apierror.New(apierror.Internal, "failed to prepare transport").
		WithCause(err).
		WithRetryable(apierror.False)
}

// NewTranscodeError creates an error for payload transcoding failures.
func NewTranscodeError(err error) error {
	return apierror.New(apierror.Internal, "failed to transcode payload").
		WithCause(err).
		WithRetryable(apierror.False)
}

// NewSchedulerStateError creates an error for invalid scheduler state.
func NewSchedulerStateError(msg string) error {
	return apierror.New(apierror.Internal, fmt.Sprintf("scheduler state error: %s", msg)).
		WithRetryable(apierror.False)
}

// NewSchedulerRewindError creates an error for rewind failures.
func NewSchedulerRewindError(err error) error {
	return apierror.New(apierror.Internal, "failed to start rewind").
		WithCause(err).
		WithRetryable(apierror.False)
}

// NewSchedulerUnwindError creates an error for unwind failures.
func NewSchedulerUnwindError(err error) error {
	return apierror.New(apierror.Internal, "failed to stop unwind").
		WithCause(err).
		WithRetryable(apierror.False)
}

// NewCompileWATError creates an error for WAT compilation failures.
func NewCompileWATError(err error) error {
	return apierror.New(apierror.Invalid, "failed to compile WAT").
		WithCause(err).
		WithRetryable(apierror.False)
}

// NewAsyncifyTransformError creates an error for asyncify transformation failures.
func NewAsyncifyTransformError(err error) error {
	return apierror.New(apierror.Internal, "failed to apply asyncify transform").
		WithCause(err).
		WithRetryable(apierror.False)
}

// NewLoadWASMError creates an error for WASM loading failures.
func NewLoadWASMError(err error) error {
	return apierror.New(apierror.Invalid, "failed to load WASM module").
		WithCause(err).
		WithRetryable(apierror.False)
}

// NewLoadComponentError creates an error for Component Model loading failures.
func NewLoadComponentError(err error) error {
	return apierror.New(apierror.Invalid, "failed to load WASM component").
		WithCause(err).
		WithRetryable(apierror.False)
}
