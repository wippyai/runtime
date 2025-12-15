package logs

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrGetLoggingConfigTimeout = apierror.New(apierror.KindTimeout, "failed to get logging config").WithRetryable(apierror.True)
	ErrSetTempConfigTimeout    = apierror.New(apierror.KindTimeout, "failed to set temporary config").WithRetryable(apierror.True)
	ErrGetConfigTimeout        = apierror.New(apierror.KindTimeout, "timeout waiting for log config").WithRetryable(apierror.True)
	ErrSetConfigTimeout        = apierror.New(apierror.KindTimeout, "timeout waiting for config confirmation").WithRetryable(apierror.True)
)

func NewContextCanceledError(err error) apierror.Error {
	return apierror.New(apierror.KindCanceled, "context canceled: "+err.Error()).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewSubscriberError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create subscriber: "+err.Error()).
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewConfigMismatchError(requested, got string) apierror.Error {
	return apierror.New(apierror.KindInternal, "config mismatch - requested: "+requested+", got: "+got).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"requested": requested, "got": got}))
}

func NewGetLoggingConfigError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to get logging config: "+err.Error()).
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewSetTempConfigError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to set temporary config: "+err.Error()).
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}
