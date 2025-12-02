package topology

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Error implements apierror.Error for topology errors
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
	ErrEmptyPatternPath = &Error{kind: apierror.KindInvalid, message: "pattern path cannot be empty", retryable: apierror.False}
)

// NewEntryExistsError creates an error when an entry already exists
func NewEntryExistsError(ns, name string) *Error {
	return &Error{
		kind:      apierror.KindConflict,
		message:   "entry already exists: {ns: " + ns + ", name: " + name + "}",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"namespace": ns, "name": name}),
	}
}

// NewEntryNotExistsError creates an error when an entry does not exist
func NewEntryNotExistsError(ns, name string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "entry does not exist: {ns: " + ns + ", name: " + name + "}",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"namespace": ns, "name": name}),
	}
}

// NewKindChangeError creates an error when trying to change entry kind
func NewKindChangeError(ns, name string, fromKind, toKind registry.Kind) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "cannot change entry kind from " + fromKind + " to " + toKind + " for {ns: " + ns + ", name: " + name + "}",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"namespace": ns, "name": name, "from_kind": fromKind, "to_kind": toKind}),
	}
}

// NewDeleteNonExistentError creates an error when trying to delete a non-existent entry
func NewDeleteNonExistentError(ns, name string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "cannot delete non-existent entry: {ns: " + ns + ", name: " + name + "}",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"namespace": ns, "name": name}),
	}
}

// NewUnknownOperationKindError creates an error for unknown operation kind
func NewUnknownOperationKindError(kind string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unknown operation kind: " + kind,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"operation_kind": kind}),
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

// NewOriginalEntryNotFoundError creates an error when original entry is not found for inverse operation
func NewOriginalEntryNotFoundError(ns, name string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "original entry not found for Process {ns: " + ns + ", name: " + name + "}",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"namespace": ns, "name": name}),
	}
}

// NewGetVersionsError creates an error when getting versions fails
func NewGetVersionsError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to get versions from history: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewNoVersionsFoundError creates an error when no versions are found
func NewNoVersionsFoundError() *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "no versions found in history",
		retryable: apierror.False,
	}
}

// NewComputePathError creates an error when computing version path fails
func NewComputePathError(targetVersion string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to get path from root to version " + targetVersion + ": " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"target_version": targetVersion, "cause": err.Error()}),
		cause:     err,
	}
}

// NewGetChangesetError creates an error when getting a changeset fails
func NewGetChangesetError(version string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to get changeset for version " + version + ": " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"version": version, "cause": err.Error()}),
		cause:     err,
	}
}
