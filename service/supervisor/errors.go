package supervisor

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error represents a supervisor service error.
type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}
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
)

func newRegisterPIDError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "register supervisor pid",
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

func newDecodeConfigError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "decode config",
		retryable: apierror.False,
		cause:     cause,
	}
}

func newInvalidEntryKindError(got, expected string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid entry kind",
		retryable: apierror.False,
		details: attrs.Bag{
			"got":      got,
			"expected": expected,
		},
	}
}

func newServiceNotFoundError(id string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "service not found",
		retryable: apierror.False,
		details:   attrs.Bag{"id": id},
	}
}
