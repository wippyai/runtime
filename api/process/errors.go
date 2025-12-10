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
	KindInternal      Kind = "Internal"
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

	ErrTerminated = &Error{
		kind:    KindInternal,
		message: "process terminated",
	}

	ErrSchedulerStopping = &Error{
		kind:    KindInvalidState,
		message: "scheduler is stopping",
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

// NewFactoryNotFoundError creates an error for missing factory.
func NewFactoryNotFoundError(factoryID string) *Error {
	return &Error{
		kind:    KindNotFound,
		message: "no factory registered for: " + factoryID,
		details: attrs.NewBagFrom(map[string]any{"factory_id": factoryID}),
	}
}

// NewInvalidFactoryEntryError creates an error for invalid factory entry.
func NewInvalidFactoryEntryError(factoryID string) *Error {
	return &Error{
		kind:    KindInternal,
		message: "invalid factory entry for: " + factoryID,
		details: attrs.NewBagFrom(map[string]any{"factory_id": factoryID}),
	}
}

// NewProcessCreateError creates an error for process creation failures.
func NewProcessCreateError(err error) *Error {
	return &Error{
		kind:    KindInternal,
		message: "failed to create process: " + err.Error(),
		details: attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:   err,
	}
}

// NewHostNotFoundError creates an error for missing host.
func NewHostNotFoundError(hostID string) *Error {
	return &Error{
		kind:    KindNotFound,
		message: "host not found: " + hostID,
		details: attrs.NewBagFrom(map[string]any{"host_id": hostID}),
	}
}

// NewInvalidHostError creates an error for host that doesn't implement process.Host.
func NewInvalidHostError(hostID string) *Error {
	return &Error{
		kind:    KindInternal,
		message: "host " + hostID + " does not implement process.Host",
		details: attrs.NewBagFrom(map[string]any{"host_id": hostID}),
	}
}

// NewSubscriberError creates an error for event subscriber failures.
func NewSubscriberError(err error) *Error {
	return &Error{
		kind:    KindInternal,
		message: "failed to create subscriber: " + err.Error(),
		details: attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:   err,
	}
}
