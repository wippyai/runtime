package app

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrAppNotInitialized = apierror.New(apierror.KindInternal, "application not initialized").WithRetryable(apierror.False)
)

func NewInitializeAppError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to initialize application").WithCause(cause).WithRetryable(apierror.False)
}

func NewStartAppError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to start application").WithCause(cause).WithRetryable(apierror.False)
}

func NewStopAppError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to stop application").WithCause(cause).WithRetryable(apierror.False)
}

func NewCreateLoggerError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create logger").WithCause(cause).WithRetryable(apierror.False)
}
