package json

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

func NewMaxDepthExceededError(maxDepth int) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "exceeded maximum depth",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"max_depth": maxDepth}),
	}
}

func NewSparseArrayError(maxKey, actualCount int) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "sparse array detected",
		retryable: apierror.False,
		details: attrs.NewBagFrom(map[string]any{
			"max_key":      maxKey,
			"actual_count": actualCount,
		}),
	}
}

func NewCompileSchemaError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "compile schema",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewConvertDataError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "convert data",
		retryable: apierror.False,
		cause:     cause,
	}
}
