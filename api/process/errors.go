package process

import (
	"strconv"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/dispatcher"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error kind constants.
const (
	KindLimitExceeded Kind = "LimitExceeded"
	KindNotFound      Kind = "NotFound"
	KindInvalidState  Kind = "InvalidState"
	KindInternal      Kind = "Internal"
)

// Errors returned by process operations.
var (
	ErrMaxProcessesExceeded = &Error{
		Kind:    KindLimitExceeded,
		Message: "max processes limit exceeded",
	}

	ErrProcessClosed = &Error{
		Kind:    KindInvalidState,
		Message: "process closed",
	}

	ErrProcessNotFound = &Error{
		Kind:    KindNotFound,
		Message: "process not found",
	}

	ErrProcessNotIdle = &Error{
		Kind:    KindInvalidState,
		Message: "process is not idle",
	}

	ErrSchedulerStopping = &Error{
		Kind:    KindInvalidState,
		Message: "scheduler is stopping",
	}
)

// UnknownCommandError indicates an unregistered command.
type UnknownCommandError struct {
	CmdID   dispatcher.CommandID
	details attrs.Attributes
}

// NewUnknownCommandError creates an error for unregistered commands.
func NewUnknownCommandError(cmdID dispatcher.CommandID) *UnknownCommandError {
	return &UnknownCommandError{
		CmdID:   cmdID,
		details: attrs.NewBagFrom(map[string]any{"command_id": int(cmdID)}),
	}
}

func (e *UnknownCommandError) Error() string {
	return "unknown command: " + strconv.Itoa(int(e.CmdID))
}

func (e *UnknownCommandError) Kind() apierror.Kind {
	return apierror.KindNotFound
}

func (e *UnknownCommandError) Retryable() apierror.Ternary {
	return apierror.False
}

func (e *UnknownCommandError) Details() attrs.Attributes {
	return e.details
}

type (
	// Kind categorizes process errors.
	Kind string

	// Error represents a process error with metadata.
	Error struct {
		Kind    Kind
		Message string
		Details attrs.Attributes
		Cause   error
	}
)

func (e *Error) Error() string                { return e.Message }
func (e *Error) GetKind() Kind                { return e.Kind }
func (e *Error) GetDetails() attrs.Attributes { return e.Details }
func (e *Error) Unwrap() error                { return e.Cause }

// WithCause returns a new error with the given cause.
func (e *Error) WithCause(cause error) *Error {
	return &Error{
		Kind:    e.Kind,
		Message: e.Message,
		Details: e.Details,
		Cause:   cause,
	}
}

// WithDetails returns a new error with the given details.
func (e *Error) WithDetails(details attrs.Attributes) *Error {
	return &Error{
		Kind:    e.Kind,
		Message: e.Message,
		Details: details,
		Cause:   e.Cause,
	}
}

// NewFactoryNotFoundError creates an error for missing factory.
func NewFactoryNotFoundError(factoryID string) *Error {
	return &Error{
		Kind:    KindNotFound,
		Message: "no factory registered for: " + factoryID,
		Details: attrs.NewBagFrom(map[string]any{"factory_id": factoryID}),
	}
}

// NewHostNotFoundError creates an error for missing host.
func NewHostNotFoundError(hostID string) *Error {
	return &Error{
		Kind:    KindNotFound,
		Message: "host not found: " + hostID,
		Details: attrs.NewBagFrom(map[string]any{"host_id": hostID}),
	}
}
