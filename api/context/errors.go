package context

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

var ErrFrameSealed = &Error{
	kind:      apierror.KindInvalid,
	message:   "frame is sealed",
	retryable: apierror.False,
}

func NewFrameSealedError(key any) *Error {
	details := attrs.NewBag()
	details.Set("key", key)
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "cannot set key in sealed frame",
		retryable: apierror.False,
		details:   details,
	}
}
