package native

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrProcessNotRunning = apierror.New(apierror.KindInvalid, "process is not running").WithRetryable(apierror.False)
	ErrProcessNotStarted = apierror.New(apierror.KindInvalid, "process not started").WithRetryable(apierror.False)
	ErrInvalidPID        = apierror.New(apierror.KindInvalid, "pid is not a positive int, process is possibly not running").WithRetryable(apierror.False)
)

func NewCommandNotAllowedError(cmd string) apierror.Error {
	return apierror.New(apierror.KindPermissionDenied, fmt.Sprintf("command not in whitelist: %s", cmd)).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"command": cmd}))
}

// ExitError represents a process exit with non-zero code
type ExitError struct {
	Code    int
	details attrs.Attributes
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("process exited with code %d", e.Code)
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
