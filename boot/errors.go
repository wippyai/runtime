package boot

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrAppContextNotInitialized = apierror.New(apierror.Internal, "app context not initialized").WithRetryable(apierror.False)

	ErrLoggerNotInitialized = apierror.New(apierror.Internal, "logger not initialized").WithRetryable(apierror.False)
)

func NewComponentAlreadyRegisteredError(name string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "component already registered: "+name).WithRetryable(apierror.False)
}

func NewDependencyResolutionError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "dependency resolution failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewComponentNotFoundError(name string) apierror.Error {
	return apierror.New(apierror.NotFound, "component not found: "+name).WithRetryable(apierror.False)
}

func NewComponentLoadError(name string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load component: "+name).WithCause(cause).WithRetryable(apierror.False)
}

func NewRuntimeServicesStartError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to start runtime services").WithCause(cause).WithRetryable(apierror.False)
}

func NewComponentStartError(name string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to start component: "+name).WithCause(cause).WithRetryable(apierror.False)
}

func NewComponentStopError(name string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to stop component: "+name).WithCause(cause).WithRetryable(apierror.False)
}

func NewShutdownError(failedCount int, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("shutdown failed (%d components failed)", failedCount)).WithCause(cause).WithRetryable(apierror.False)
}
