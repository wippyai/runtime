package wasm

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
	ErrSourceRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "source is required",
		retryable: apierror.False,
	}

	ErrMethodRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "method is required",
		retryable: apierror.False,
	}

	ErrFSRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "fs is required",
		retryable: apierror.False,
	}

	ErrPathRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "path is required",
		retryable: apierror.False,
	}

	ErrHashRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "hash is required",
		retryable: apierror.False,
	}

	ErrInvalidPoolSize = &Error{
		kind:      apierror.KindInvalid,
		message:   "pool.size must be greater than 0 for non-lazy/inline pools",
		retryable: apierror.False,
	}
)
