package native

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrProcessNotRunning = apierror.New(apierror.Invalid, "process is not running").WithRetryable(apierror.False)
	ErrProcessNotStarted = apierror.New(apierror.Invalid, "process not started").WithRetryable(apierror.False)
	ErrInvalidPID        = apierror.New(apierror.Invalid, "pid is not a positive int, process is possibly not running").WithRetryable(apierror.False)
)

func NewCommandNotAllowedError(cmd string) apierror.Error {
	return apierror.New(apierror.PermissionDenied, "command not allowed").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"command": cmd}))
}

// ExitError represents a process exit with non-zero code
type ExitError struct {
	details attrs.Attributes
	Code    int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("process exited with code %d", e.Code)
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
