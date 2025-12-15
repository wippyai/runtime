package supervisor

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrNoRelayNode      = apierror.New(apierror.Internal, "no relay node in context").WithRetryable(apierror.False)
	ErrNoTopology       = apierror.New(apierror.Internal, "no topology in context").WithRetryable(apierror.False)
	ErrNoProcessManager = apierror.New(apierror.Internal, "no process manager in context").WithRetryable(apierror.False)
)

func newRegisterPIDError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "register supervisor pid").WithCause(cause)
}

func newAttachRelayError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "attach to relay").WithCause(cause)
}

func newStartProcessError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "start process").WithCause(cause)
}

func newSendCancelError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "send cancel").WithCause(cause)
}

func newDecodeConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "decode config").WithRetryable(apierror.False).WithCause(cause)
}

func newInvalidEntryKindError(got, expected string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid entry kind").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"got": got, "expected": expected})
}

func newServiceNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "service not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.Bag{"id": id})
}
