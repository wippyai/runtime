package runner

import (
	"time"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Sentinel errors
var (
	ErrUnrelatedAcceptEvent = apierror.New(apierror.KindInvalid, "unrelated accept event")
	ErrUnrelatedRejectEvent = apierror.New(apierror.KindInvalid, "unrelated reject event")
)

// NewOperationFailedError creates an error when an operation fails
func NewOperationFailedError(err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"operation failed: "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewInvalidOperationError creates an error when an operation is invalid
func NewInvalidOperationError(err error) apierror.Error {
	return apierror.E(
		apierror.KindInvalid,
		"invalid operation: "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewEntryKindNotFoundError creates an error when entry kind is not found
func NewEntryKindNotFoundError(entryID registry.ID) apierror.Error {
	return apierror.E(
		apierror.KindNotFound,
		"entry kind not found: "+entryID.String(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"entry_id": entryID.String()}),
		nil,
	)
}

// NewApplyChangeError creates an error when applying a change fails
func NewApplyChangeError(err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"applying change to state: "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewOperationRejectedError creates an error when an operation is rejected
func NewOperationRejectedError(entryID registry.ID, err error) apierror.Error {
	if err == nil {
		return apierror.E(
			apierror.KindInvalid,
			"operation rejected for entry "+entryID.String()+", no details",
			apierror.False,
			attrs.NewBagFrom(map[string]any{"entry_id": entryID.String()}),
			nil,
		)
	}
	return apierror.E(
		apierror.KindInvalid,
		"operation failed for entry "+entryID.String()+": "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"entry_id": entryID.String(), "cause": err.Error()}),
		err,
	)
}

// NewOperationCanceledError creates an error when an operation is canceled
func NewOperationCanceledError(entryID registry.ID, kind registry.Kind, err error) apierror.Error {
	return apierror.E(
		apierror.KindCanceled,
		"operation context canceled for "+entryID.String()+" ("+kind+"): "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"entry_id": entryID.String(), "kind": kind, "cause": err.Error()}),
		err,
	)
}

// NewEventHandlerTimeoutError creates an error when event handler times out
func NewEventHandlerTimeoutError(timeout time.Duration, entryID registry.ID, kind registry.Kind) apierror.Error {
	return apierror.E(
		apierror.KindTimeout,
		"event handler timeout after "+timeout.String()+" for entry "+entryID.String()+" (kind: "+kind+"): no listener responded - check if listener is registered for this kind",
		apierror.True,
		attrs.NewBagFrom(map[string]any{"timeout": timeout.String(), "entry_id": entryID.String(), "kind": kind}),
		nil,
	)
}

// NewListenEventsError creates an error when subscribing to events fails
func NewListenEventsError(err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"listening events: "+err.Error(),
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}
