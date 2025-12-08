package process

import (
	"fmt"
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
)

// Errors returned by process operations.
var (
	ErrMaxProcessesExceeded = &Error{
		kind:    KindLimitExceeded,
		message: "max processes limit exceeded",
	}

	ErrProcessClosed = fmt.Errorf("process closed")

	ErrProcessNotFound = &Error{
		kind:    KindNotFound,
		message: "process not found",
	}

	ErrProcessNotIdle = &Error{
		kind:    KindInvalidState,
		message: "process is not idle",
	}
)

// UnknownCommandError indicates an unregistered command.
type UnknownCommandError struct {
	CmdID   dispatcher.CommandID
	details attrs.Attributes
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
	if e.details == nil {
		e.details = attrs.NewBagFrom(map[string]any{"command_id": int(e.CmdID)})
	}
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
