package queue

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Sentinel errors for queue operations.
var (
	ErrDriverNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "queue driver not found",
		retryable: apierror.False,
	}

	ErrQueueNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "queue not found",
		retryable: apierror.False,
	}

	ErrDriverNotStarted = &Error{
		kind:      apierror.KindUnavailable,
		message:   "queue driver not started",
		retryable: apierror.True,
	}

	ErrQueueFull = &Error{
		kind:      apierror.KindUnavailable,
		message:   "queue is full",
		retryable: apierror.True,
	}

	ErrQueueClosed = &Error{
		kind:      apierror.KindUnavailable,
		message:   "queue is closed",
		retryable: apierror.False,
	}

	ErrMessageExpired = &Error{
		kind:      apierror.KindInvalid,
		message:   "message expired",
		retryable: apierror.False,
	}

	ErrConsumerClosed = &Error{
		kind:      apierror.KindUnavailable,
		message:   "consumer closed",
		retryable: apierror.False,
	}

	ErrNoPublishFunc = &Error{
		kind:      apierror.KindUnavailable,
		message:   "no publish function configured",
		retryable: apierror.False,
	}

	ErrDriverIDRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "driver ID is required",
		retryable: apierror.False,
	}

	ErrQueueIDRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "queue ID is required",
		retryable: apierror.False,
	}

	ErrFunctionIDRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "function ID is required",
		retryable: apierror.False,
	}
)

// Error represents a queue error with metadata.
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

// WithCause returns a new error with the given cause.
func (e *Error) WithCause(cause error) *Error {
	return &Error{
		kind:      e.kind,
		message:   e.message,
		retryable: e.retryable,
		details:   e.details,
		cause:     cause,
	}
}

// WithMessage returns a new error with a custom message.
func (e *Error) WithMessage(msg string) *Error {
	return &Error{
		kind:      e.kind,
		message:   msg,
		retryable: e.retryable,
		details:   e.details,
		cause:     e.cause,
	}
}

// WithDetails returns a new error with additional details.
func (e *Error) WithDetails(details attrs.Attributes) *Error {
	return &Error{
		kind:      e.kind,
		message:   e.message,
		retryable: e.retryable,
		details:   details,
		cause:     e.cause,
	}
}

// NewDriverNotFoundError creates a driver not found error with ID.
func NewDriverNotFoundError(id registry.ID) *Error {
	details := attrs.NewBag()
	details.Set("driver_id", id.String())
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "driver not found: " + id.String(),
		retryable: apierror.False,
		details:   details,
	}
}

// NewQueueNotFoundError creates a queue not found error with ID.
func NewQueueNotFoundError(id registry.ID) *Error {
	details := attrs.NewBag()
	details.Set("queue_id", id.String())
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "queue not found: " + id.String(),
		retryable: apierror.False,
		details:   details,
	}
}

// NewDriverExistsError creates a driver already exists error.
func NewDriverExistsError(id registry.ID) *Error {
	details := attrs.NewBag()
	details.Set("driver_id", id.String())
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "driver already exists: " + id.String(),
		retryable: apierror.False,
		details:   details,
	}
}

// NewQueueClosedError creates a queue closed error with ID.
func NewQueueClosedError(id registry.ID) *Error {
	details := attrs.NewBag()
	details.Set("queue_id", id.String())
	return &Error{
		kind:      apierror.KindUnavailable,
		message:   "queue is closed: " + id.String(),
		retryable: apierror.False,
		details:   details,
	}
}

// NewConfigError creates a configuration error.
func NewConfigError(msg string, cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   msg,
		retryable: apierror.False,
		cause:     cause,
	}
}

// NewUnsupportedKindError creates an unsupported entry kind error.
func NewUnsupportedKindError(kind string) *Error {
	details := attrs.NewBag()
	details.Set("kind", kind)
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported entry kind: " + kind,
		retryable: apierror.False,
		details:   details,
	}
}

// NewConcurrencyExceededError creates a concurrency limit error.
func NewConcurrencyExceededError(value, max int) *Error {
	details := attrs.NewBag()
	details.Set("concurrency", value)
	details.Set("max", max)
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "concurrency exceeds maximum",
		retryable: apierror.False,
		details:   details,
	}
}

// NewPrefetchExceededError creates a prefetch limit error.
func NewPrefetchExceededError(value, max int) *Error {
	details := attrs.NewBag()
	details.Set("prefetch", value)
	details.Set("max", max)
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "prefetch exceeds maximum",
		retryable: apierror.False,
		details:   details,
	}
}
