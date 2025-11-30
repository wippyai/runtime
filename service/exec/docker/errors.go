package docker

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error implements apierror.Error for Docker executor errors
type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
}

// Error implements error interface
func (e *Error) Error() string {
	return e.message
}

// Kind implements apierror.Error
func (e *Error) Kind() apierror.Kind {
	return e.kind
}

// Retryable implements apierror.Error
func (e *Error) Retryable() apierror.Ternary {
	return e.retryable
}

// Details implements apierror.Error
func (e *Error) Details() attrs.Attributes {
	return e.details
}

// Sentinel errors
var (
	ErrContainerNotStarted = &Error{
		kind:      apierror.KindInvalid,
		message:   "container not started",
		retryable: apierror.False,
	}

	ErrContainerAlreadyStart = &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "container already started",
		retryable: apierror.False,
	}

	ErrContainerStopped = &Error{
		kind:      apierror.KindInvalid,
		message:   "container already stopped",
		retryable: apierror.False,
	}

	ErrImageRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "docker image is required",
		retryable: apierror.False,
	}

	ErrStdinNotAvailable = &Error{
		kind:      apierror.KindUnavailable,
		message:   "stdin not available",
		retryable: apierror.False,
	}
)

// NewCommandNotAllowedError creates an error for rejected commands
func NewCommandNotAllowedError(cmd string) *Error {
	return &Error{
		kind:      apierror.KindPermissionDenied,
		message:   fmt.Sprintf("command not in whitelist: %s", cmd),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"command": cmd}),
	}
}

// ExitError represents a container exit with non-zero code
type ExitError struct {
	Code    int
	details attrs.Attributes
}

// Error implements error interface
func (e *ExitError) Error() string {
	return fmt.Sprintf("container exited with code %d", e.Code)
}

// Kind implements apierror.Error
func (e *ExitError) Kind() apierror.Kind {
	if e.Code == 137 || e.Code == 143 {
		// SIGKILL (137) or SIGTERM (143) - canceled
		return apierror.KindCanceled
	}
	return apierror.KindInternal
}

// Retryable implements apierror.Error
func (e *ExitError) Retryable() apierror.Ternary {
	// Non-zero exit codes are generally not retryable
	return apierror.False
}

// Details implements apierror.Error
func (e *ExitError) Details() attrs.Attributes {
	if e.details == nil {
		e.details = attrs.NewBagFrom(map[string]any{"exit_code": e.Code})
	}
	return e.details
}

// ExitCode returns the exit code
func (e *ExitError) ExitCode() int {
	return e.Code
}

// WrapError wraps a standard error with Docker error context
func WrapError(kind apierror.Kind, err error, retryable apierror.Ternary) *Error {
	return &Error{
		kind:      kind,
		message:   err.Error(),
		retryable: retryable,
	}
}
