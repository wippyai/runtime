// SPDX-License-Identifier: MPL-2.0

package expansion

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Sentinel error for invalid directive results.
var ErrDirectiveResultInvalid = apierror.New(apierror.Internal, "directive returned data without Applied=true").
	WithRetryable(apierror.False)

// Sentinel error for conflicting expanded operations.
var ErrDirectiveExpansionConflict = apierror.New(apierror.Internal, "expansion operation conflicts with original entry").
	WithRetryable(apierror.False)

// NewDirectiveResultInvalidError returns a structured error for invalid directive output.
func NewDirectiveResultInvalidError(entryID registry.ID, entryKind registry.Kind) apierror.Error {
	return apierror.New(apierror.Internal, "directive returned data without Applied=true for "+entryID.String()+" (kind: "+entryKind+")").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"entry_id":   entryID.String(),
			"entry_kind": entryKind,
		}))
}

// NewDirectiveExpansionConflictError returns a structured error for conflicting expansion output.
func NewDirectiveExpansionConflictError(entryID registry.ID) apierror.Error {
	return apierror.New(apierror.Internal, "expansion produced entry "+entryID.String()+" which already exists in the changeset").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"entry_id": entryID.String(),
		}))
}

// NewSortedOperationScopeMissingError returns a structured error for impossible
// scope remapping after sorting. It should only happen if the sorter returns an
// operation that was not present in the input plan.
func NewSortedOperationScopeMissingError(entryID registry.ID, kind string) apierror.Error {
	return apierror.New(apierror.Internal, "sorted operation has no matching expansion scope").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"entry_id": entryID.String(),
			"op_kind":  kind,
		}))
}
