package runner

import (
	"time"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Sentinel errors
var (
	ErrUnrelatedAcceptEvent = apierror.New(apierror.Invalid, "unrelated accept event").WithRetryable(apierror.False)
	ErrUnrelatedRejectEvent = apierror.New(apierror.Invalid, "unrelated reject event").WithRetryable(apierror.False)
)

// NewInvalidOperationError creates an error when an operation is invalid
func NewInvalidOperationError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid operation").
		WithRetryable(apierror.False).
		WithCause(err)
}

// NewEntryKindNotFoundError creates an error when entry kind is not found
func NewEntryKindNotFoundError(entryID registry.ID) apierror.Error {
	return apierror.New(apierror.NotFound, "entry kind not found: "+entryID.String()).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"entry_id": entryID.String()}))
}

// NewApplyChangeError creates an error when applying a change fails
func NewApplyChangeError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to apply change to state").
		WithRetryable(apierror.False).
		WithCause(err)
}

// NewOperationRejectedError creates an error when an operation is rejected
func NewOperationRejectedError(entryID registry.ID, err error) apierror.Error {
	if err == nil {
		return apierror.New(apierror.Invalid, "operation rejected for entry "+entryID.String()+", no details").
			WithRetryable(apierror.False).
			WithDetails(attrs.NewBagFrom(map[string]any{"entry_id": entryID.String()}))
	}
	return apierror.New(apierror.Invalid, "operation failed for entry "+entryID.String()).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"entry_id": entryID.String()})).
		WithCause(err)
}

// NewOperationCanceledError creates an error when an operation is canceled
func NewOperationCanceledError(entryID registry.ID, kind registry.Kind, err error) apierror.Error {
	return apierror.New(apierror.Canceled, "operation context canceled for "+entryID.String()+" ("+kind+")").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"entry_id": entryID.String(), "kind": kind})).
		WithCause(err)
}

// NewEventHandlerTimeoutError creates an error when event handler times out
func NewEventHandlerTimeoutError(timeout time.Duration, entryID registry.ID, kind registry.Kind) apierror.Error {
	return apierror.New(apierror.Timeout, "event handler timeout after "+timeout.String()+" for entry "+entryID.String()+" (kind: "+kind+"): no listener responded - check if listener is registered for this kind").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"timeout": timeout.String(), "entry_id": entryID.String(), "kind": kind}))
}

// NewListenEventsError creates an error when subscribing to events fails
func NewListenEventsError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to subscribe to events").
		WithRetryable(apierror.True).
		WithCause(err)
}
