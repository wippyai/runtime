package process

import (
	"strconv"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/dispatcher"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error kind constants.
const (
	LimitExceeded Kind = "LimitExceeded"
	NotFound      Kind = "NotFound"
	InvalidState  Kind = "InvalidState"
	Internal      Kind = "Internal"
)

// Errors returned by process operations.
var (
	ErrMaxProcessesExceeded = &Error{
		kind:    LimitExceeded,
		message: "max processes limit exceeded",
	}

	ErrProcessClosed = &Error{
		kind:    InvalidState,
		message: "process closed",
	}

	ErrProcessNotFound = &Error{
		kind:    NotFound,
		message: "process not found",
	}

	ErrProcessNotIdle = &Error{
		kind:    InvalidState,
		message: "process is not idle",
	}

	ErrSchedulerStopping = &Error{
		kind:    InvalidState,
		message: "scheduler is stopping",
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
	return apierror.NotFound
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
		kind    Kind
		message string
		details attrs.Attributes
		cause   error
	}
)

func (e *Error) Error() string             { return e.message }
func (e *Error) Kind() Kind                { return e.kind }
func (e *Error) Details() attrs.Attributes { return e.details }
func (e *Error) Unwrap() error             { return e.cause }

// NewError creates a new process error with the given kind and message.
func NewError(kind Kind, message string) *Error {
	return &Error{kind: kind, message: message}
}

// WithCause returns a new error with the given cause.
func (e *Error) WithCause(cause error) *Error {
	return &Error{
		kind:    e.kind,
		message: e.message,
		details: e.details,
		cause:   cause,
	}
}

// WithDetails returns a new error with the given details.
func (e *Error) WithDetails(details attrs.Attributes) *Error {
	return &Error{
		kind:    e.kind,
		message: e.message,
		details: details,
		cause:   e.cause,
	}
}

// NewFactoryNotFoundError creates an error for missing factory.
func NewFactoryNotFoundError(factoryID string) *Error {
	return &Error{
		kind:    NotFound,
		message: "no factory registered for: " + factoryID,
		details: attrs.NewBagFrom(map[string]any{"factory_id": factoryID}),
	}
}

// NewHostNotFoundError creates an error for missing host.
func NewHostNotFoundError(hostID string) *Error {
	return &Error{
		kind:    NotFound,
		message: "host not found: " + hostID,
		details: attrs.NewBagFrom(map[string]any{"host_id": hostID}),
	}
}
