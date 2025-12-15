package queue

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Sentinel errors for queue operations.
var (
	ErrDriverNotFound     = apierror.New(apierror.KindNotFound, "queue driver not found").WithRetryable(apierror.False)
	ErrQueueNotFound      = apierror.New(apierror.KindNotFound, "queue not found").WithRetryable(apierror.False)
	ErrMessageExpired     = apierror.New(apierror.KindInvalid, "message expired").WithRetryable(apierror.False)
	ErrDriverIDRequired   = apierror.New(apierror.KindInvalid, "driver ID is required").WithRetryable(apierror.False)
	ErrQueueIDRequired    = apierror.New(apierror.KindInvalid, "queue ID is required").WithRetryable(apierror.False)
	ErrFunctionIDRequired = apierror.New(apierror.KindInvalid, "function ID is required").WithRetryable(apierror.False)
)

// NewDriverNotFoundError creates a driver not found error with ID.
func NewDriverNotFoundError(id registry.ID) apierror.Error {
	return apierror.E(
		apierror.KindNotFound,
		"driver not found: "+id.String(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"driver_id": id.String()}),
		nil,
	)
}

// NewQueueNotFoundError creates a queue not found error with ID.
func NewQueueNotFoundError(id registry.ID) apierror.Error {
	return apierror.E(
		apierror.KindNotFound,
		"queue not found: "+id.String(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"queue_id": id.String()}),
		nil,
	)
}

// NewDriverExistsError creates a driver already exists error.
func NewDriverExistsError(id registry.ID) apierror.Error {
	return apierror.E(
		apierror.KindAlreadyExists,
		"driver already exists: "+id.String(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"driver_id": id.String()}),
		nil,
	)
}

// NewConfigError creates a configuration error.
func NewConfigError(msg string, cause error) apierror.Error {
	return apierror.E(
		apierror.KindInvalid,
		msg,
		apierror.False,
		nil,
		cause,
	)
}

// NewUnsupportedKindError creates an unsupported entry kind error.
func NewUnsupportedKindError(kind string) apierror.Error {
	return apierror.E(
		apierror.KindInvalid,
		"unsupported entry kind: "+kind,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"kind": kind}),
		nil,
	)
}

// NewConcurrencyExceededError creates a concurrency limit error.
func NewConcurrencyExceededError(value, maxValue int) apierror.Error {
	return apierror.E(
		apierror.KindInvalid,
		"concurrency exceeds maximum",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"concurrency": value, "max": maxValue}),
		nil,
	)
}

// NewPrefetchExceededError creates a prefetch limit error.
func NewPrefetchExceededError(value, maxValue int) apierror.Error {
	return apierror.E(
		apierror.KindInvalid,
		"prefetch exceeds maximum",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"prefetch": value, "max": maxValue}),
		nil,
	)
}
