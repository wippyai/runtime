package registry

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Error implements apierror.Error for registry errors
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
	ErrNoCurrentVersion           = &Error{kind: apierror.KindNotFound, message: "no current version", retryable: apierror.False}
	ErrDependencyResolverNotInit  = &Error{kind: apierror.KindInternal, message: "dependency resolver not initialized", retryable: apierror.False}
	ErrBuilderNoChangesetReversal = &Error{kind: apierror.KindUnavailable, message: "builder does not support changeset reversal", retryable: apierror.False}
)

// NewEntryNotFoundError creates an error when an entry is not found
func NewEntryNotFoundError(path registry.ID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "entry not found: " + path.String(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"path": path.String()}),
	}
}

// NewApplyChangesError creates an error when applying changes fails
func NewApplyChangesError(err error, rollbackErr error) *Error {
	if rollbackErr != nil {
		return &Error{
			kind:      apierror.KindInternal,
			message:   "failed to apply changes: " + err.Error() + ", failed to rollback: " + rollbackErr.Error(),
			retryable: apierror.False,
			details:   attrs.NewBagFrom(map[string]any{"cause": err.Error(), "rollback_error": rollbackErr.Error()}),
			cause:     err,
		}
	}
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to apply changes: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewSaveVersionError creates an error when saving a new version fails
func NewSaveVersionError(err error, rollbackErr error) *Error {
	if rollbackErr != nil {
		return &Error{
			kind:      apierror.KindInternal,
			message:   "failed to save new version: " + err.Error() + ", failed to rollback: " + rollbackErr.Error(),
			retryable: apierror.False,
			details:   attrs.NewBagFrom(map[string]any{"cause": err.Error(), "rollback_error": rollbackErr.Error()}),
			cause:     err,
		}
	}
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to save new version: " + err.Error() + ", recovered",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
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

// NewVersionNotFoundError creates an error when a version is not found
func NewVersionNotFoundError(versionID uint) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "version not found in history",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"version_id": versionID}),
	}
}

// NewComputePathError creates an error when computing version path fails
func NewComputePathError(fromID, toID uint, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to compute path from v" + uintToStr(fromID) + " to v" + uintToStr(toID) + ": " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"from_version": fromID, "to_version": toID, "cause": err.Error()}),
		cause:     err,
	}
}

// NewGetChangesetError creates an error when getting a changeset fails
func NewGetChangesetError(versionID uint, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to get changeset for version v" + uintToStr(versionID) + ": " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"version_id": versionID, "cause": err.Error()}),
		cause:     err,
	}
}

// NewReverseChangesetError creates an error when reversing a changeset fails
func NewReverseChangesetError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to reverse changeset: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewApplyVersionChangesError creates an error when applying version changes fails
func NewApplyVersionChangesError(err error, rollbackErr error) *Error {
	if rollbackErr != nil {
		return &Error{
			kind:      apierror.KindInternal,
			message:   "failed to apply version changes: " + err.Error() + ", failed to rollback: " + rollbackErr.Error(),
			retryable: apierror.False,
			details:   attrs.NewBagFrom(map[string]any{"cause": err.Error(), "rollback_error": rollbackErr.Error()}),
			cause:     err,
		}
	}
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to apply version changes: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewSetHeadError creates an error when setting the head version fails
func NewSetHeadError(versionID uint, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "history set head to " + uintToStr(versionID) + ": " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"version_id": versionID, "cause": err.Error()}),
		cause:     err,
	}
}

// NewLoadStateError creates an error when loading state fails
func NewLoadStateError(err error, rollbackErr error) *Error {
	if rollbackErr != nil {
		return &Error{
			kind:      apierror.KindInternal,
			message:   "failed to load state: " + err.Error() + ", failed to rollback: " + rollbackErr.Error(),
			retryable: apierror.False,
			details:   attrs.NewBagFrom(map[string]any{"cause": err.Error(), "rollback_error": rollbackErr.Error()}),
			cause:     err,
		}
	}
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to load state: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewComputeTransitionError creates an error when computing state transition fails
func NewComputeTransitionError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to compute transition: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// uintToStr converts uint to string without importing strconv
func uintToStr(n uint) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
