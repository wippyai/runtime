package boot

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.message, e.cause)
	}
	return e.message
}
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

var (
	ErrAppContextNotInitialized = &Error{
		kind:    apierror.KindInternal,
		message: "AppContext not initialized - call NewInfrastructure first",
	}
	ErrLoggerNotInitialized = &Error{
		kind:    apierror.KindInternal,
		message: "logger not initialized - call NewInfrastructure first",
	}
)

func NewComponentAlreadyRegisteredError(name string) *Error {
	return &Error{
		kind:    apierror.KindAlreadyExists,
		message: fmt.Sprintf("component %q already registered", name),
	}
}

func NewDependencyResolutionError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "dependency resolution failed",
		cause:   cause,
	}
}

func NewComponentNotFoundError(name string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: fmt.Sprintf("component %q not found", name),
	}
}

func NewComponentLoadError(name string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("component %q load failed", name),
		cause:   cause,
	}
}

func NewRuntimeServicesStartError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to start runtime services",
		cause:   cause,
	}
}

func NewComponentStartError(name string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("component %q start failed", name),
		cause:   cause,
	}
}

func NewComponentStopError(name string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("component %q stop failed", name),
		cause:   cause,
	}
}

func NewShutdownError(count int, firstError error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("shutdown errors (%d components)", count),
		cause:   firstError,
	}
}
