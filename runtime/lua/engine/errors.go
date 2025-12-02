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

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

var ErrChannelNotFound = &Error{
	kind:      apierror.KindNotFound,
	message:   "channel not found",
	retryable: apierror.No,
}

var ErrProcessNotInitialized = &Error{
	kind:      apierror.KindInternal,
	message:   "process not initialized",
	retryable: apierror.No,
}

var ErrTaskNotFound = &Error{
	kind:      apierror.KindNotFound,
	message:   "task not found",
	retryable: apierror.No,
}

var ErrProcessContextNotAvailable = &Error{
	kind:      apierror.KindInternal,
	message:   "process context not available",
	retryable: apierror.No,
}

var ErrNoScriptOrProto = &Error{
	kind:      apierror.KindInvalid,
	message:   "no script or proto provided",
	retryable: apierror.No,
}

var ErrStateNotInitialized = &Error{
	kind:      apierror.KindInternal,
	message:   "process state not initialized - use Factory.Create()",
	retryable: apierror.No,
}

func NewTopicAlreadySubscribedError(topic string) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "topic \"" + topic + "\" already subscribed",
		retryable: apierror.No,
	}
}

func NewStoreResourcesError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to store resources",
		retryable: apierror.No,
		cause:     cause,
	}
}

func NewStoreProcessContextError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to store process context",
		retryable: apierror.No,
		cause:     cause,
	}
}

func NewLoadScriptError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to load script",
		retryable: apierror.No,
		cause:     cause,
	}
}

func NewExecuteScriptError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to execute script",
		retryable: apierror.No,
		cause:     cause,
	}
}

func NewMethodNotFoundError(method string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "method \"" + method + "\" not found in module",
		retryable: apierror.No,
	}
}

func NewTaskNotFoundForChannelError(cause error) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "task not found for channel result",
		retryable: apierror.No,
		cause:     cause,
	}
}
