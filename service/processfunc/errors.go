package processfunc

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error represents a process function error.
type Error struct {
	kind    apierror.Kind
	message string
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return apierror.Unknown }
func (e *Error) Details() attrs.Attributes   { return nil }
func (e *Error) Unwrap() error               { return e.cause }

var ErrMonitorChannelClosed = &Error{
	kind:    apierror.KindInternal,
	message: "monitor channel closed unexpectedly",
}

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
