package topology

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Sentinel errors
var (
	ErrEmptyPatternPath = apierror.New(apierror.KindInvalid, "pattern path cannot be empty")
)

// NewEntryExistsError creates an error when an entry already exists
func NewEntryExistsError(ns, name string) apierror.Error {
	return apierror.E(
		apierror.KindConflict,
		"entry already exists: {ns: "+ns+", name: "+name+"}",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"namespace": ns, "name": name}),
		nil,
	)
}

// NewEntryNotExistsError creates an error when an entry does not exist
func NewEntryNotExistsError(ns, name string) apierror.Error {
	return apierror.E(
		apierror.KindNotFound,
		"entry does not exist: {ns: "+ns+", name: "+name+"}",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"namespace": ns, "name": name}),
		nil,
	)
}

// NewKindChangeError creates an error when trying to change entry kind
func NewKindChangeError(ns, name string, fromKind, toKind registry.Kind) apierror.Error {
	return apierror.E(
		apierror.KindInvalid,
		"cannot change entry kind from "+fromKind+" to "+toKind+" for {ns: "+ns+", name: "+name+"}",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"namespace": ns, "name": name, "from_kind": fromKind, "to_kind": toKind}),
		nil,
	)
}

// NewDeleteNonExistentError creates an error when trying to delete a non-existent entry
func NewDeleteNonExistentError(ns, name string) apierror.Error {
	return apierror.E(
		apierror.KindNotFound,
		"cannot delete non-existent entry: {ns: "+ns+", name: "+name+"}",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"namespace": ns, "name": name}),
		nil,
	)
}

// NewUnknownOperationKindError creates an error for unknown operation kind
func NewUnknownOperationKindError(kind string) apierror.Error {
	return apierror.E(
		apierror.KindInvalid,
		"unknown operation kind: "+kind,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"operation_kind": kind}),
		nil,
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

// NewOriginalEntryNotFoundError creates an error when original entry is not found for inverse operation
func NewOriginalEntryNotFoundError(ns, name string) apierror.Error {
	return apierror.E(
		apierror.KindNotFound,
		"original entry not found for Process {ns: "+ns+", name: "+name+"}",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"namespace": ns, "name": name}),
		nil,
	)
}

// NewGetVersionsError creates an error when getting versions fails
func NewGetVersionsError(err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"failed to get versions from history: "+err.Error(),
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewNoVersionsFoundError creates an error when no versions are found
func NewNoVersionsFoundError() apierror.Error {
	return apierror.E(
		apierror.KindNotFound,
		"no versions found in history",
		apierror.False,
		nil,
		nil,
	)
}

// NewComputePathError creates an error when computing version path fails
func NewComputePathError(targetVersion string, err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"failed to get path from root to version "+targetVersion+": "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"target_version": targetVersion, "cause": err.Error()}),
		err,
	)
}

// NewGetChangesetError creates an error when getting a changeset fails
func NewGetChangesetError(version string, err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"failed to get changeset for version "+version+": "+err.Error(),
		apierror.True,
		attrs.NewBagFrom(map[string]any{"version": version, "cause": err.Error()}),
		err,
	)
}

// NewApplyOperationError creates an error when applying an operation fails during state building
func NewApplyOperationError(version string, entryID string, err error) apierror.Error {
	return apierror.E(
		apierror.KindInternal,
		"failed to apply operation for entry "+entryID+" at version "+version+": "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"version": version, "entry_id": entryID, "cause": err.Error()}),
		err,
	)
}
