package logs

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrGetLoggingConfigTimeout = apierror.New(apierror.Timeout, "failed to get logging config").WithRetryable(apierror.True)
	ErrSetTempConfigTimeout    = apierror.New(apierror.Timeout, "failed to set temporary config").WithRetryable(apierror.True)
	ErrGetConfigTimeout        = apierror.New(apierror.Timeout, "timeout waiting for log config").WithRetryable(apierror.True)
	ErrSetConfigTimeout        = apierror.New(apierror.Timeout, "timeout waiting for config confirmation").WithRetryable(apierror.True)
)

func NewContextCanceledError(err error) apierror.Error {
	return apierror.New(apierror.Canceled, "context canceled").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewSubscriberError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create subscriber").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewConfigMismatchError(requested, got string) apierror.Error {
	return apierror.New(apierror.Internal, "config mismatch").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"requested": requested, "got": got}))
}

func NewGetLoggingConfigError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get logging config").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewSetTempConfigError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to set temporary config").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}
