// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func newError(kind apierror.Kind, message string, retryable apierror.Ternary, details attrs.Attributes, cause error) apierror.Error {
	builder := apierror.New(kind, message).WithRetryable(retryable)
	if details != nil {
		builder = builder.WithDetails(details)
	}
	if cause != nil {
		builder = builder.WithCause(cause)
	}
	return builder
}

func NewInvalidConfigError(message string) apierror.Error {
	return newError(apierror.Invalid, message, apierror.False, nil, nil)
}

func NewOpenDatabaseError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to open PostgreSQL history database", apierror.True, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewConnectError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to connect to PostgreSQL history database", apierror.True, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewMigrationError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to run PostgreSQL history migrations", apierror.False, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewEnsureRootVersionError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to ensure root version", apierror.False, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewCheckRootVersionError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to check root version", apierror.True, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewInsertRootVersionError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to insert root version", apierror.False, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewInsertChangesetError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to insert changeset", apierror.False, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewSetInitialHeadError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to set initial head", apierror.False, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewQueryVersionsError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to query versions", apierror.True, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewScanVersionError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to scan version", apierror.False, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewInvalidParentVersionError(parentID int64) apierror.Error {
	return newError(apierror.Internal, "invalid negative parent version ID", apierror.False, attrs.NewBagFrom(map[string]any{"parent_id": parentID}), nil)
}

func NewParentVersionNotFoundError(parentID, versionID uint) apierror.Error {
	return newError(apierror.NotFound, "parent version not found for version", apierror.False, attrs.NewBagFrom(map[string]any{"parent_id": parentID, "version_id": versionID}), nil)
}

func NewIterateVersionsError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to iterate versions", apierror.True, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewChangesetNotFoundError(versionID uint) apierror.Error {
	return newError(apierror.NotFound, "changeset not found for version", apierror.False, attrs.NewBagFrom(map[string]any{"version_id": versionID}), nil)
}

func NewQueryChangesetError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to query changeset", apierror.True, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewDecodeChangesetError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to decode changeset", apierror.False, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewBeginTransactionError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to begin transaction", apierror.True, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewParentVersionIDTooLargeError(prevID uint) apierror.Error {
	return newError(apierror.Internal, "parent version ID too large", apierror.False, attrs.NewBagFrom(map[string]any{"parent_id": prevID}), nil)
}

func NewInsertVersionError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to insert version", apierror.False, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewEncodeChangesetError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to encode changeset", apierror.False, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewUpdateHeadError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to update head", apierror.False, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewCommitTransactionError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to commit transaction", apierror.True, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewQueryHeadError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to query head", apierror.True, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewParseHeadError(value string, err error) apierror.Error {
	return newError(apierror.Internal, "failed to parse head version", apierror.False, attrs.NewBagFrom(map[string]any{"value": value, "cause": err.Error()}), err)
}

func NewGetVersionsError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to get versions", apierror.True, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewHeadVersionNotFoundError(headID uint) apierror.Error {
	return newError(apierror.NotFound, "head version not found", apierror.False, attrs.NewBagFrom(map[string]any{"head_id": headID}), nil)
}

func NewSetHeadError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to set head", apierror.False, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}

func NewCloseDatabaseError(err error) apierror.Error {
	return newError(apierror.Internal, "failed to close database", apierror.False, attrs.NewBagFrom(map[string]any{"cause": err.Error()}), err)
}
