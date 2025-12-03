package engine

import (
	"errors"
	"fmt" // Note: fmt kept for Sprintf in logging

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// DeadlockError indicates all coroutines are blocked with no pending operations.
type DeadlockError struct {
	ThreadCount int
	Message     string
}

func (e *DeadlockError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("deadlock: %s (threads=%d)", e.Message, e.ThreadCount)
	}
	return fmt.Sprintf("deadlock: all %d coroutines blocked with no pending operations", e.ThreadCount)
}

// IsDeadlock returns true if err is a DeadlockError.
func IsDeadlock(err error) bool {
	var deadlock *DeadlockError
	return errors.As(err, &deadlock)
}

type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

var ErrChannelNotFound = &Error{
	kind:      apierror.KindNotFound,
	message:   "channel not found",
	retryable: apierror.False,
}

var ErrProcessNotInitialized = &Error{
	kind:      apierror.KindInternal,
	message:   "process not initialized",
	retryable: apierror.False,
}

var ErrTaskNotFound = &Error{
	kind:      apierror.KindNotFound,
	message:   "task not found",
	retryable: apierror.False,
}

var ErrProcessContextNotAvailable = &Error{
	kind:      apierror.KindInternal,
	message:   "process context not available",
	retryable: apierror.False,
}

var ErrNoScriptOrProto = &Error{
	kind:      apierror.KindInvalid,
	message:   "no script or proto provided",
	retryable: apierror.False,
}

var ErrStateNotInitialized = &Error{
	kind:      apierror.KindInternal,
	message:   "process state not initialized - use Factory.Create()",
	retryable: apierror.False,
}

func NewTopicAlreadySubscribedError(topic string) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "topic \"" + topic + "\" already subscribed",
		retryable: apierror.False,
	}
}

func NewStoreResourcesError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to store resources",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewStoreProcessContextError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to store process context",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewLoadScriptError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to load script",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewExecuteScriptError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to execute script",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewMethodNotFoundError(method string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "method \"" + method + "\" not found in module",
		retryable: apierror.False,
	}
}

func NewTaskNotFoundForChannelError(cause error) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "task not found for channel result",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewOperationError(operation string, cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   operation,
		retryable: apierror.False,
		cause:     cause,
	}
}
