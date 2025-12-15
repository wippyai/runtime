package queue

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Sentinel errors for queue operations.
var (
	ErrDriverNotStarted = apierror.New(apierror.Unavailable, "queue driver not started").WithRetryable(apierror.True)
	ErrQueueFull        = apierror.New(apierror.Unavailable, "queue is full").WithRetryable(apierror.True)
	ErrQueueClosed      = apierror.New(apierror.Unavailable, "queue is closed").WithRetryable(apierror.False)
	ErrConsumerClosed   = apierror.New(apierror.Unavailable, "consumer closed").WithRetryable(apierror.False)
	ErrNoPublishFunc    = apierror.New(apierror.Unavailable, "no publish function configured").WithRetryable(apierror.False)
)

// NewQueueClosedError creates a queue closed error with ID.
func NewQueueClosedError(id registry.ID) apierror.Error {
	return apierror.E(
		apierror.Unavailable,
		"queue is closed: "+id.String(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"queue_id": id.String()}),
		nil,
	)
}

// NewDriverNotFoundError creates a driver not found error with ID.
func NewDriverNotFoundError(id registry.ID) apierror.Error {
	return apierror.E(
		apierror.NotFound,
		"driver not found: "+id.String(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"driver_id": id.String()}),
		nil,
	)
}

// NewQueueNotFoundError creates a queue not found error with ID.
func NewQueueNotFoundError(id registry.ID) apierror.Error {
	return apierror.E(
		apierror.NotFound,
		"queue not found: "+id.String(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"queue_id": id.String()}),
		nil,
	)
}

// NewDriverExistsError creates a driver already exists error.
func NewDriverExistsError(id registry.ID) apierror.Error {
	return apierror.E(
		apierror.AlreadyExists,
		"driver already exists: "+id.String(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"driver_id": id.String()}),
		nil,
	)
}

// NewConfigError creates a configuration error.
func NewConfigError(msg string, cause error) apierror.Error {
	return apierror.E(
		apierror.Invalid,
		msg,
		apierror.False,
		nil,
		cause,
	)
}

// NewUnsupportedKindError creates an unsupported entry kind error.
func NewUnsupportedKindError(kind string) apierror.Error {
	return apierror.E(
		apierror.Invalid,
		"unsupported entry kind: "+kind,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"kind": kind}),
		nil,
	)
}

// NewConcurrencyExceededError creates a concurrency limit error.
func NewConcurrencyExceededError(value, maxValue int) apierror.Error {
	return apierror.E(
		apierror.Invalid,
		"concurrency exceeds maximum",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"concurrency": value, "max": maxValue}),
		nil,
	)
}

// NewPrefetchExceededError creates a prefetch limit error.
func NewPrefetchExceededError(value, maxValue int) apierror.Error {
	return apierror.E(
		apierror.Invalid,
		"prefetch exceeds maximum",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"prefetch": value, "max": maxValue}),
		nil,
	)
}
