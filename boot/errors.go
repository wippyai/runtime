package boot

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrBootstrapFailed = apierror.New(apierror.KindInternal, "bootstrap failed").WithRetryable(apierror.False)
)

func NewLoadComponentError(component string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to load component: "+component).WithCause(cause).WithRetryable(apierror.False)
}

func NewInitializeComponentError(component string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to initialize component: "+component).WithCause(cause).WithRetryable(apierror.False)
}

var (
	ErrAppContextNotInitialized = apierror.New(apierror.KindInternal, "app context not initialized").WithRetryable(apierror.False)

	ErrLoggerNotInitialized = apierror.New(apierror.KindInternal, "logger not initialized").WithRetryable(apierror.False)
)

func NewComponentAlreadyRegisteredError(name string) apierror.Error {
	return apierror.New(apierror.KindAlreadyExists, "component already registered: "+name).WithRetryable(apierror.False)
}

func NewDependencyResolutionError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "dependency resolution failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewComponentNotFoundError(name string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "component not found: "+name).WithRetryable(apierror.False)
}

func NewComponentLoadError(name string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to load component: "+name).WithCause(cause).WithRetryable(apierror.False)
}

func NewRuntimeServicesStartError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to start runtime services").WithCause(cause).WithRetryable(apierror.False)
}

func NewComponentStartError(name string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to start component: "+name).WithCause(cause).WithRetryable(apierror.False)
}

func NewComponentStopError(name string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to stop component: "+name).WithCause(cause).WithRetryable(apierror.False)
}

func NewShutdownError(failedCount int, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, fmt.Sprintf("shutdown failed (%d components failed)", failedCount)).WithCause(cause).WithRetryable(apierror.False)
}
