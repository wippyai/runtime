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
	cause     error
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

// Sentinel errors for docker container operations
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

func (e *ExitError) Error() string {
	return fmt.Sprintf("container exited with code %d", e.Code)
}

func (e *ExitError) Kind() apierror.Kind {
	if e.Code == 137 || e.Code == 143 {
		return apierror.KindCanceled
	}
	return apierror.KindInternal
}

func (e *ExitError) Retryable() apierror.Ternary { return apierror.False }

func (e *ExitError) Details() attrs.Attributes {
	if e.details == nil {
		e.details = attrs.NewBagFrom(map[string]any{"exit_code": e.Code})
	}
	return e.details
}

func (e *ExitError) ExitCode() int { return e.Code }

// NewDockerClientError creates an error for Docker client creation failures
func NewDockerClientError(err error) *Error {
	return &Error{
		kind:      apierror.KindUnavailable,
		message:   fmt.Sprintf("failed to create docker client: %v", err),
		retryable: apierror.True,
		cause:     err,
	}
}

// NewContainerCreateError creates an error for container creation failures
func NewContainerCreateError(err error) *Error {
	return &Error{
		kind:      apierror.KindUnavailable,
		message:   fmt.Sprintf("failed to create container: %v", err),
		retryable: apierror.True,
		cause:     err,
	}
}

// NewContainerAttachError creates an error for container attach failures
func NewContainerAttachError(err error) *Error {
	return &Error{
		kind:      apierror.KindUnavailable,
		message:   fmt.Sprintf("failed to attach to container: %v", err),
		retryable: apierror.True,
		cause:     err,
	}
}

// NewContainerStartError creates an error for container start failures
func NewContainerStartError(err error) *Error {
	return &Error{
		kind:      apierror.KindUnavailable,
		message:   fmt.Sprintf("failed to start container: %v", err),
		retryable: apierror.True,
		cause:     err,
	}
}

// NewSignalError creates an error for signal send failures
func NewSignalError(err error) *Error {
	return &Error{
		kind:      apierror.KindUnavailable,
		message:   fmt.Sprintf("failed to send signal: %v", err),
		retryable: apierror.False,
		cause:     err,
	}
}
