package runner

import (
	"time"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Error implements apierror.Error for runner errors
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

// Sentinel errors
var (
	ErrUnrelatedAcceptEvent = &Error{kind: apierror.KindInvalid, message: "unrelated accept event", retryable: apierror.False}
	ErrUnrelatedRejectEvent = &Error{kind: apierror.KindInvalid, message: "unrelated reject event", retryable: apierror.False}
)

// NewOperationFailedError creates an error when an operation fails
func NewOperationFailedError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "operation failed: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewInvalidOperationError creates an error when an operation is invalid
func NewInvalidOperationError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid operation: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewEntryKindNotFoundError creates an error when entry kind is not found
func NewEntryKindNotFoundError(entryID registry.ID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "entry kind not found: " + entryID.String(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"entry_id": entryID.String()}),
	}
}

// NewApplyChangeError creates an error when applying a change fails
func NewApplyChangeError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "applying change to state: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewOperationRejectedError creates an error when an operation is rejected
func NewOperationRejectedError(entryID registry.ID, err error) *Error {
	if err == nil {
		return &Error{
			kind:      apierror.KindInvalid,
			message:   "operation rejected for entry " + entryID.String() + ", no details",
			retryable: apierror.False,
			details:   attrs.NewBagFrom(map[string]any{"entry_id": entryID.String()}),
		}
	}
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "operation failed for entry " + entryID.String() + ": " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"entry_id": entryID.String(), "cause": err.Error()}),
		cause:     err,
	}
}

// NewOperationCanceledError creates an error when an operation is canceled
func NewOperationCanceledError(entryID registry.ID, kind registry.Kind, err error) *Error {
	return &Error{
		kind:      apierror.KindCanceled,
		message:   "operation context canceled for " + entryID.String() + " (" + kind + "): " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"entry_id": entryID.String(), "kind": kind, "cause": err.Error()}),
		cause:     err,
	}
}

// NewEventHandlerTimeoutError creates an error when event handler times out
func NewEventHandlerTimeoutError(timeout time.Duration, entryID registry.ID, kind registry.Kind) *Error {
	return &Error{
		kind:      apierror.KindTimeout,
		message:   "event handler timeout after " + timeout.String() + " for entry " + entryID.String() + " (kind: " + kind + "): no listener responded - check if listener is registered for this kind",
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"timeout": timeout.String(), "entry_id": entryID.String(), "kind": kind}),
	}
}

// NewListenEventsError creates an error when subscribing to events fails
func NewListenEventsError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "listening events: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}
