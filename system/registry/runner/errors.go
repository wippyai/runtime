package runner

import (
	"time"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Sentinel errors
var (
	ErrUnrelatedAcceptEvent = apierror.New(apierror.Invalid, "unrelated accept event")
	ErrUnrelatedRejectEvent = apierror.New(apierror.Invalid, "unrelated reject event")
)

// NewOperationFailedError creates an error when an operation fails
func NewOperationFailedError(err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"operation failed",
		apierror.False,
		nil,
		err,
	)
}

// NewInvalidOperationError creates an error when an operation is invalid
func NewInvalidOperationError(err error) apierror.Error {
	return apierror.E(
		apierror.Invalid,
		"invalid operation",
		apierror.False,
		nil,
		err,
	)
}

// NewEntryKindNotFoundError creates an error when entry kind is not found
func NewEntryKindNotFoundError(entryID registry.ID) apierror.Error {
	return apierror.E(
		apierror.NotFound,
		"entry kind not found: "+entryID.String(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"entry_id": entryID.String()}),
		nil,
	)
}

// NewApplyChangeError creates an error when applying a change fails
func NewApplyChangeError(err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"applying change to state",
		apierror.False,
		nil,
		err,
	)
}

// NewOperationRejectedError creates an error when an operation is rejected
func NewOperationRejectedError(entryID registry.ID, err error) apierror.Error {
	if err == nil {
		return apierror.E(
			apierror.Invalid,
			"operation rejected for entry "+entryID.String()+", no details",
			apierror.False,
			attrs.NewBagFrom(map[string]any{"entry_id": entryID.String()}),
			nil,
		)
	}
	return apierror.E(
		apierror.Invalid,
		"operation failed for entry "+entryID.String(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"entry_id": entryID.String()}),
		err,
	)
}

// NewOperationCanceledError creates an error when an operation is canceled
func NewOperationCanceledError(entryID registry.ID, kind registry.Kind, err error) apierror.Error {
	return apierror.E(
		apierror.Canceled,
		"operation context canceled for "+entryID.String()+" ("+kind+")",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"entry_id": entryID.String(), "kind": kind}),
		err,
	)
}

// NewEventHandlerTimeoutError creates an error when event handler times out
func NewEventHandlerTimeoutError(timeout time.Duration, entryID registry.ID, kind registry.Kind) apierror.Error {
	return apierror.E(
		apierror.Timeout,
		"event handler timeout after "+timeout.String()+" for entry "+entryID.String()+" (kind: "+kind+"): no listener responded - check if listener is registered for this kind",
		apierror.True,
		attrs.NewBagFrom(map[string]any{"timeout": timeout.String(), "entry_id": entryID.String(), "kind": kind}),
		nil,
	)
}

// NewListenEventsError creates an error when subscribing to events fails
func NewListenEventsError(err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"listening events",
		apierror.True,
		nil,
		err,
	)
}
