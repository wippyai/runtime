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

// Unwrap implements error unwrapping
func (e *Error) Unwrap() error {
	return e.cause
}

// Sentinel errors
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

// Error implements error interface
func (e *ExitError) Error() string {
	return fmt.Sprintf("process exited with code %d", e.Code)
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

// WrapError wraps a standard error with native executor error context
func WrapError(kind apierror.Kind, err error, retryable apierror.Ternary) *Error {
	return &Error{
		kind:      kind,
		message:   err.Error(),
		retryable: retryable,
		cause:     err,
	}
}

// NewUnsupportedEntryKindError creates an error for unsupported entry kinds
func NewUnsupportedEntryKindError(kind string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   fmt.Sprintf("unsupported entry kind: %s", kind),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"kind": kind}),
	}
}

// NewExecutorAlreadyExistsError creates an error when executor already exists
func NewExecutorAlreadyExistsError(id string) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   fmt.Sprintf("executor %s already exists", id),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"executor_id": id}),
	}
}

// NewExecutorNotFoundError creates an error when executor is not found
func NewExecutorNotFoundError(id string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   fmt.Sprintf("executor %s not found", id),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"executor_id": id}),
	}
}

// NewConfigDecodeError creates an error for configuration decode failures
func NewConfigDecodeError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   fmt.Sprintf("failed to decode configuration: %v", err),
		retryable: apierror.False,
		cause:     err,
	}
}

// NewExecutorCreateError creates an error for executor creation failures
func NewExecutorCreateError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   fmt.Sprintf("failed to create executor: %v", err),
		retryable: apierror.True,
		cause:     err,
	}
}
