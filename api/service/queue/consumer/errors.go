package consumer

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

func NewConcurrencyExceededError(concurrency, max int) *Error {
	details := attrs.NewBag()
	details.Set("concurrency", concurrency)
	details.Set("max", max)
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "concurrency exceeds maximum",
		retryable: apierror.False,
		details:   details,
	}
}

func NewPrefetchExceededError(prefetch, max int) *Error {
	details := attrs.NewBag()
	details.Set("prefetch", prefetch)
	details.Set("max", max)
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "prefetch exceeds maximum",
		retryable: apierror.False,
		details:   details,
	}
}
