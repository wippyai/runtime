// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Sentinel errors
var (
	ErrNoCurrentVersion          = apierror.New(apierror.NotFound, "no current version").WithRetryable(apierror.False)
	ErrDependencyResolverNotInit = apierror.New(apierror.Internal, "dependency resolver not initialized").WithRetryable(apierror.False)
	ErrEmptyVersionPath          = apierror.New(apierror.Internal, "empty version path").WithRetryable(apierror.False)
	ErrNoCommonAncestor          = apierror.New(apierror.Internal, "no common ancestor found in version path").WithRetryable(apierror.False)
)

// NewEntryNotFoundError creates an error when an entry is not found
func NewEntryNotFoundError(path registry.ID) apierror.Error {
	return apierror.New(apierror.NotFound, "entry not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path.String()}))
}

// NewApplyChangesError creates an error when applying changes fails
func NewApplyChangesError(err error, rollbackErr error) apierror.Error {
	if rollbackErr != nil {
		return apierror.New(apierror.Internal, "failed to apply changes").
			WithRetryable(apierror.False).
			WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error(), "rollback_error": rollbackErr.Error()})).
			WithCause(err)
	}
	return apierror.New(apierror.Internal, "failed to apply changes").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

// NewSaveVersionError creates an error when saving a new version fails
func NewSaveVersionError(err error, rollbackErr error) apierror.Error {
	if rollbackErr != nil {
		return apierror.New(apierror.Internal, "failed to save new version").
			WithRetryable(apierror.False).
			WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error(), "rollback_error": rollbackErr.Error()})).
			WithCause(err)
	}
	return apierror.New(apierror.Internal, "failed to save new version").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

// NewExpandChangesError creates an error when changeset expansion fails.
func NewExpandChangesError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to expand changeset").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

// NewSortChangesError creates an error when changeset ordering fails.
func NewSortChangesError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to sort changeset").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

// NewPrepareEffectsError creates an error when preparing effects fails.
func NewPrepareEffectsError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to prepare effects").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

// NewCommitEffectsError creates an error when committing effects fails.
func NewCommitEffectsError(err error, rollbackErr error) apierror.Error {
	if rollbackErr != nil {
		return apierror.New(apierror.Internal, "failed to commit effects").
			WithRetryable(apierror.False).
			WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error(), "rollback_error": rollbackErr.Error()})).
			WithCause(err)
	}
	return apierror.New(apierror.Internal, "failed to commit effects").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

// NewConcurrentApplyError creates an error when registry state changes mid-apply.
func NewConcurrentApplyError(expected, actual uint) apierror.Error {
	return apierror.New(apierror.Conflict, "registry changed during apply").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"expected_version": expected, "actual_version": actual}))
}

// NewGetVersionsError creates an error when getting versions fails
func NewGetVersionsError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get versions from history").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

// NewVersionNotFoundError creates an error when a version is not found
func NewVersionNotFoundError(versionID uint) apierror.Error {
	return apierror.New(apierror.NotFound, "version not found in history").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"version_id": versionID}))
}

// NewComputePathError creates an error when computing version path fails
func NewComputePathError(fromID, toID uint, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to compute version path").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"from_version": fromID, "to_version": toID, "cause": err.Error()})).
		WithCause(err)
}

// NewGetChangesetError creates an error when getting a changeset fails
func NewGetChangesetError(versionID uint, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get changeset").
		WithRetryable(apierror.True).
		WithDetails(attrs.NewBagFrom(map[string]any{"version_id": versionID, "cause": err.Error()})).
		WithCause(err)
}

// NewReverseChangesetError creates an error when reversing a changeset fails
func NewReverseChangesetError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to reverse changeset").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

// NewApplyVersionChangesError creates an error when applying version changes fails
func NewApplyVersionChangesError(err error, rollbackErr error) apierror.Error {
	if rollbackErr != nil {
		return apierror.New(apierror.Internal, "failed to apply version changes").
			WithRetryable(apierror.False).
			WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error(), "rollback_error": rollbackErr.Error()})).
			WithCause(err)
	}
	return apierror.New(apierror.Internal, "failed to apply version changes").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

// NewSetHeadError creates an error when setting the head version fails
func NewSetHeadError(versionID uint, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to set head version").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"version_id": versionID, "cause": err.Error()})).
		WithCause(err)
}

// NewLoadStateError creates an error when loading state fails
func NewLoadStateError(err error, rollbackErr error) apierror.Error {
	if rollbackErr != nil {
		return apierror.New(apierror.Internal, "failed to load state").
			WithRetryable(apierror.False).
			WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error(), "rollback_error": rollbackErr.Error()})).
			WithCause(err)
	}
	return apierror.New(apierror.Internal, "failed to load state").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

// NewComputeTransitionError creates an error when computing state transition fails
func NewComputeTransitionError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to compute transition").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}
