// SPDX-License-Identifier: MPL-2.0

package runner

import (
	"strconv"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
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

// NewTransactionTimeoutError creates an error when transaction listeners do not acknowledge a lifecycle event.
func NewTransactionTimeoutError(kind event.Kind, timeout time.Duration, expected, accepted int) apierror.Error {
	return apierror.New(apierror.Timeout, "transaction handler timeout after "+timeout.String()+" for "+kind+": accepted "+strconv.Itoa(accepted)+" of "+strconv.Itoa(expected)).
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"timeout":  timeout.String(),
			"kind":     kind,
			"expected": expected,
			"accepted": accepted,
		}))
}

// NewDuplicateTransactionParticipantError creates an error for ambiguous transaction reply identities.
func NewDuplicateTransactionParticipantError(id string) apierror.Error {
	return apierror.New(apierror.Invalid, "duplicate transaction participant id: "+id).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"participant_id": id}))
}

// NewAwaitServiceMissingError creates an error when a runner is used without the await service.
func NewAwaitServiceMissingError() apierror.Error {
	return apierror.New(apierror.Internal, "event await service is required for registry event coordination").
		WithRetryable(apierror.False)
}

// NewTransactionRejectedError creates an error when a transaction listener rejects a lifecycle event.
func NewTransactionRejectedError(kind event.Kind, err error) apierror.Error {
	if err == nil {
		return apierror.New(apierror.Invalid, "transaction rejected for "+kind).
			WithRetryable(apierror.False).
			WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
	}
	return apierror.New(apierror.Invalid, "transaction rejected for "+kind).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind})).
		WithCause(err)
}

// NewListenEventsError creates an error when subscribing to events fails
func NewListenEventsError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to subscribe to events").
		WithRetryable(apierror.True).
		WithCause(err)
}

// NewUnresolvedDependenciesError is returned by Transition when the
// deferred-and-retry apply loop cannot make further progress: every remaining
// operation rejects with a NotFound error and no provider entry showed up in
// the same changeset to satisfy it. The wrapped cause is the first rejection
// from the final pass; ids of all unresolved entries are recorded in details.
func NewUnresolvedDependenciesError(unresolved []registry.ID, cause error) apierror.Error {
	ids := make([]string, len(unresolved))
	for i, id := range unresolved {
		ids[i] = id.String()
	}
	e := apierror.New(apierror.NotFound, "unresolved dependencies after retry: "+strings.Join(ids, ",")).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"unresolved": ids}))
	if cause != nil {
		e = e.WithCause(cause)
	}
	return e
}
