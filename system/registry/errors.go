package registry

import (
	"strconv"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Sentinel errors
var (
	ErrNoCurrentVersion          = apierror.New(apierror.NotFound, "no current version")
	ErrDependencyResolverNotInit = apierror.New(apierror.Internal, "dependency resolver not initialized")
	ErrEmptyVersionPath          = apierror.New(apierror.Internal, "empty version path")
	ErrNoCommonAncestor          = apierror.New(apierror.Internal, "no common ancestor found in version path")
)

// NewEntryNotFoundError creates an error when an entry is not found
func NewEntryNotFoundError(path registry.ID) apierror.Error {
	return apierror.E(
		apierror.NotFound,
		"entry not found: "+path.String(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"path": path.String()}),
		nil,
	)
}

// NewApplyChangesError creates an error when applying changes fails
func NewApplyChangesError(err error, rollbackErr error) apierror.Error {
	if rollbackErr != nil {
		return apierror.E(
			apierror.Internal,
			"failed to apply changes, rollback failed: "+rollbackErr.Error(),
			apierror.False,
			attrs.NewBagFrom(map[string]any{"cause": err.Error(), "rollback_error": rollbackErr.Error()}),
			err,
		)
	}
	return apierror.E(
		apierror.Internal,
		"failed to apply changes",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewSaveVersionError creates an error when saving a new version fails
func NewSaveVersionError(err error, rollbackErr error) apierror.Error {
	if rollbackErr != nil {
		return apierror.E(
			apierror.Internal,
			"failed to save new version, rollback failed: "+rollbackErr.Error(),
			apierror.False,
			attrs.NewBagFrom(map[string]any{"cause": err.Error(), "rollback_error": rollbackErr.Error()}),
			err,
		)
	}
	return apierror.E(
		apierror.Internal,
		"failed to save new version, recovered",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewGetVersionsError creates an error when getting versions fails
func NewGetVersionsError(err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to get versions from history",
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewVersionNotFoundError creates an error when a version is not found
func NewVersionNotFoundError(versionID uint) apierror.Error {
	return apierror.E(
		apierror.NotFound,
		"version not found in history",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"version_id": versionID}),
		nil,
	)
}

// NewComputePathError creates an error when computing version path fails
func NewComputePathError(fromID, toID uint, err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to compute path from v"+strconv.FormatUint(uint64(fromID), 10)+" to v"+strconv.FormatUint(uint64(toID), 10),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"from_version": fromID, "to_version": toID, "cause": err.Error()}),
		err,
	)
}

// NewGetChangesetError creates an error when getting a changeset fails
func NewGetChangesetError(versionID uint, err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to get changeset for version v"+strconv.FormatUint(uint64(versionID), 10),
		apierror.True,
		attrs.NewBagFrom(map[string]any{"version_id": versionID, "cause": err.Error()}),
		err,
	)
}

// NewReverseChangesetError creates an error when reversing a changeset fails
func NewReverseChangesetError(err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to reverse changeset",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewApplyVersionChangesError creates an error when applying version changes fails
func NewApplyVersionChangesError(err error, rollbackErr error) apierror.Error {
	if rollbackErr != nil {
		return apierror.E(
			apierror.Internal,
			"failed to apply version changes, rollback failed: "+rollbackErr.Error(),
			apierror.False,
			attrs.NewBagFrom(map[string]any{"cause": err.Error(), "rollback_error": rollbackErr.Error()}),
			err,
		)
	}
	return apierror.E(
		apierror.Internal,
		"failed to apply version changes",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewSetHeadError creates an error when setting the head version fails
func NewSetHeadError(versionID uint, err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"history set head to v"+strconv.FormatUint(uint64(versionID), 10),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"version_id": versionID, "cause": err.Error()}),
		err,
	)
}

// NewLoadStateError creates an error when loading state fails
func NewLoadStateError(err error, rollbackErr error) apierror.Error {
	if rollbackErr != nil {
		return apierror.E(
			apierror.Internal,
			"failed to load state, rollback failed: "+rollbackErr.Error(),
			apierror.False,
			attrs.NewBagFrom(map[string]any{"cause": err.Error(), "rollback_error": rollbackErr.Error()}),
			err,
		)
	}
	return apierror.E(
		apierror.Internal,
		"failed to load state",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewComputeTransitionError creates an error when computing state transition fails
func NewComputeTransitionError(err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to compute transition",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}
