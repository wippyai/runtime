package supervisor

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/pid"
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
	ErrProcessRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "process Process is required",
		retryable: apierror.False,
	}

	ErrHostRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "host Process is required",
		retryable: apierror.False,
	}
)

func NewInvalidHostError(hostID pid.HostID) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   fmt.Sprintf("host Process cannot be %s", hostID),
		retryable: apierror.False,
	}
}
