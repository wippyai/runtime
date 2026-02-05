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
	return apierror.SetDetails(ErrDirectiveResultInvalid, attrs.NewBagFrom(map[string]any{
		"entry_id":   entryID.String(),
		"entry_kind": string(entryKind),
	}))
}

// NewDirectiveExpansionConflictError returns a structured error for conflicting expansion output.
func NewDirectiveExpansionConflictError(entryID registry.ID) apierror.Error {
	return apierror.SetDetails(ErrDirectiveExpansionConflict, attrs.NewBagFrom(map[string]any{
		"entry_id": entryID.String(),
	}))
}
