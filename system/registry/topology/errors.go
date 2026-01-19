package topology

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// ErrEmptyPatternPath is a sentinel error
var (
	ErrEmptyPatternPath = apierror.New(apierror.Invalid, "pattern path cannot be empty").WithRetryable(apierror.False)
)

// NewEntryExistsError creates an error when an entry already exists
func NewEntryExistsError(ns, name string) apierror.Error {
	return apierror.New(apierror.Conflict, "entry already exists: {ns: "+ns+", name: "+name+"}").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"namespace": ns, "name": name}))
}

// NewEntryNotExistsError creates an error when an entry does not exist
func NewEntryNotExistsError(ns, name string) apierror.Error {
	return apierror.New(apierror.NotFound, "entry does not exist: {ns: "+ns+", name: "+name+"}").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"namespace": ns, "name": name}))
}

// NewKindChangeError creates an error when trying to change entry kind
func NewKindChangeError(ns, name string, fromKind, toKind registry.Kind) apierror.Error {
	return apierror.New(apierror.Invalid, "cannot change entry kind from "+fromKind+" to "+toKind+" for {ns: "+ns+", name: "+name+"}").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"namespace": ns, "name": name, "from_kind": fromKind, "to_kind": toKind}))
}

// NewDeleteNonExistentError creates an error when trying to delete a non-existent entry
func NewDeleteNonExistentError(ns, name string) apierror.Error {
	return apierror.New(apierror.NotFound, "cannot delete non-existent entry: {ns: "+ns+", name: "+name+"}").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"namespace": ns, "name": name}))
}

// NewUnknownOperationKindError creates an error for unknown operation kind
func NewUnknownOperationKindError(kind string) apierror.Error {
	return apierror.New(apierror.Invalid, "unknown operation kind: "+kind).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"operation_kind": kind}))
}

// NewInvalidOperationError creates an error when an operation is invalid
func NewInvalidOperationError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid operation: "+err.Error()).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

// NewOriginalEntryNotFoundError creates an error when original entry is not found for inverse operation
func NewOriginalEntryNotFoundError(ns, name string) apierror.Error {
	return apierror.New(apierror.NotFound, "original entry not found for Process {ns: "+ns+", name: "+name+"}").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"namespace": ns, "name": name}))
}

// NewGetVersionsError creates an error when getting versions fails
func NewGetVersionsError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get versions from history: "+err.Error()).
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

// NewNoVersionsFoundError creates an error when no versions are found
func NewNoVersionsFoundError() apierror.Error {
	return apierror.New(apierror.NotFound, "no versions found in history").
		WithRetryable(apierror.False)
}

// NewComputePathError creates an error when computing version path fails
func NewComputePathError(targetVersion string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get path from root to version "+targetVersion+": "+err.Error()).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"target_version": targetVersion, "cause": err.Error()})).
		WithCause(err)
}

// NewGetChangesetError creates an error when getting a changeset fails
func NewGetChangesetError(version string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get changeset for version "+version+": "+err.Error()).
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"version": version, "cause": err.Error()})).
		WithCause(err)
}

// NewApplyOperationError creates an error when applying an operation fails during state building
func NewApplyOperationError(version string, entryID string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to apply operation for entry "+entryID+" at version "+version+": "+err.Error()).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"version": version, "entry_id": entryID, "cause": err.Error()})).
		WithCause(err)
}
