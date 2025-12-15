package docker

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrContainerNotStarted   = apierror.New(apierror.Invalid, "container not started").WithRetryable(apierror.False)
	ErrContainerAlreadyStart = apierror.New(apierror.AlreadyExists, "container already started").WithRetryable(apierror.False)
	ErrContainerStopped      = apierror.New(apierror.Invalid, "container already stopped").WithRetryable(apierror.False)
	ErrStdinNotAvailable     = apierror.New(apierror.Unavailable, "stdin not available").WithRetryable(apierror.False)
)

func NewCommandNotAllowedError(cmd string) apierror.Error {
	return apierror.New(apierror.PermissionDenied, fmt.Sprintf("command not in whitelist: %s", cmd)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"command": cmd}))
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
		return apierror.Canceled
	}
	return apierror.Internal
}

func (e *ExitError) Retryable() apierror.Ternary { return apierror.False }

func (e *ExitError) Details() attrs.Attributes {
	if e.details == nil {
		e.details = attrs.NewBagFrom(map[string]any{"exit_code": e.Code})
	}
	return e.details
}

func (e *ExitError) ExitCode() int { return e.Code }

func NewDockerClientError(err error) apierror.Error {
	return apierror.New(apierror.Unavailable, fmt.Sprintf("failed to create docker client: %v", err)).
		WithRetryable(apierror.True).
		WithCause(err)
}

func NewContainerCreateError(err error) apierror.Error {
	return apierror.New(apierror.Unavailable, fmt.Sprintf("failed to create container: %v", err)).
		WithRetryable(apierror.True).
		WithCause(err)
}

func NewContainerAttachError(err error) apierror.Error {
	return apierror.New(apierror.Unavailable, fmt.Sprintf("failed to attach to container: %v", err)).
		WithRetryable(apierror.True).
		WithCause(err)
}

func NewContainerStartError(err error) apierror.Error {
	return apierror.New(apierror.Unavailable, fmt.Sprintf("failed to start container: %v", err)).
		WithRetryable(apierror.True).
		WithCause(err)
}

func NewSignalError(err error) apierror.Error {
	return apierror.New(apierror.Unavailable, fmt.Sprintf("failed to send signal: %v", err)).
		WithRetryable(apierror.False).
		WithCause(err)
}
