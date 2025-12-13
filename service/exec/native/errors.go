package native

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error implements apierror.Error for native executor errors
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

// Sentinel errors for native process operations
var (
	ErrProcessNotRunning = &Error{
		kind:      apierror.KindInvalid,
		message:   "process is not running",
		retryable: apierror.False,
	}

	ErrProcessNotStarted = &Error{
		kind:      apierror.KindInvalid,
		message:   "process not started",
		retryable: apierror.False,
	}

	ErrInvalidPID = &Error{
		kind:      apierror.KindInvalid,
		message:   "pid is not a positive int, process is possibly not running",
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
