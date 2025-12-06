package processfunc

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error represents a process function error.
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
	ErrNoRelayNode = &Error{
		kind:    apierror.KindInternal,
		message: "no relay node in context",
	}

	ErrNoTopology = &Error{
		kind:    apierror.KindInternal,
		message: "no topology in context",
	}

	ErrNoProcessManager = &Error{
		kind:    apierror.KindInternal,
		message: "no process manager in context",
	}

	ErrMonitorChannelClosed = &Error{
		kind:    apierror.KindInternal,
		message: "monitor channel closed unexpectedly",
	}
)

func newRegisterPIDError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "register caller pid",
		cause:   cause,
	}
}

func newAttachRelayError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "attach to relay",
		cause:   cause,
	}
}

func newStartProcessError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "start process",
		cause:   cause,
	}
}
