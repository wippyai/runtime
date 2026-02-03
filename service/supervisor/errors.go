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
	apiErr := apierror.New(apierror.Internal, "register supervisor pid").
		WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func newAttachRelayError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "attach to relay").
		WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func newStartProcessError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "start process").
		WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func newSendCancelError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Internal, "send cancel").
		WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func newDecodeConfigError(cause error) apierror.Error {
	apiErr := apierror.New(apierror.Invalid, "decode config").WithRetryable(apierror.False)
	if cause != nil {
		apiErr = apiErr.WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).WithCause(cause)
	}
	return apiErr
}

func newInvalidEntryKindError(got, expected string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid entry kind").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"got":      got,
			"expected": expected,
		}))
}

func newServiceNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "service not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}
