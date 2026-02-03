package sqlite

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

// NewOpenDatabaseError creates an error when opening database fails
func NewOpenDatabaseError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to open database",
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewConnectError creates an error when connecting to database fails
func NewConnectError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to connect to database",
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewMigrationError creates an error when running migrations fails
func NewMigrationError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to run migrations",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewEnsureRootVersionError creates an error when ensuring root version fails
func NewEnsureRootVersionError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to ensure root version",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewCheckRootVersionError creates an error when checking root version fails
func NewCheckRootVersionError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to check root version",
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewInsertRootVersionError creates an error when inserting root version fails
func NewInsertRootVersionError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to insert root version",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewInsertChangesetError creates an error when inserting changeset fails
func NewInsertChangesetError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to insert changeset",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewSetInitialHeadError creates an error when setting initial head fails
func NewSetInitialHeadError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to set initial head",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewQueryVersionsError creates an error when querying versions fails
func NewQueryVersionsError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to query versions",
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewScanVersionError creates an error when scanning version row fails
func NewScanVersionError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to scan version",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewInvalidParentVersionError creates an error for invalid parent version ID
func NewInvalidParentVersionError(parentID int64) apierror.Error {
	return newError(
		apierror.Internal,
		"invalid negative parent version ID",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"parent_id": parentID}),
		nil,
	)
}

// NewParentVersionNotFoundError creates an error when parent version is not found
func NewParentVersionNotFoundError(parentID, versionID uint) apierror.Error {
	return newError(
		apierror.NotFound,
		"parent version not found for version",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"parent_id": parentID, "version_id": versionID}),
		nil,
	)
}

// NewIterateVersionsError creates an error when iterating versions fails
func NewIterateVersionsError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to iterate versions",
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewChangesetNotFoundError creates an error when changeset is not found
func NewChangesetNotFoundError(versionID uint) apierror.Error {
	return newError(
		apierror.NotFound,
		"changeset not found for version",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"version_id": versionID}),
		nil,
	)
}

// NewQueryChangesetError creates an error when querying changeset fails
func NewQueryChangesetError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to query changeset",
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewDecodeChangesetError creates an error when decoding changeset fails
func NewDecodeChangesetError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to decode changeset",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewBeginTransactionError creates an error when beginning transaction fails
func NewBeginTransactionError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to begin transaction",
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewParentVersionIDTooLargeError creates an error when parent version ID is too large
func NewParentVersionIDTooLargeError(prevID uint) apierror.Error {
	return newError(
		apierror.Internal,
		"parent version ID too large",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"parent_id": prevID}),
		nil,
	)
}

// NewInsertVersionError creates an error when inserting version fails
func NewInsertVersionError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to insert version",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewEncodeChangesetError creates an error when encoding changeset fails
func NewEncodeChangesetError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to encode changeset",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewUpdateHeadError creates an error when updating head fails
func NewUpdateHeadError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to update head",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewCommitTransactionError creates an error when committing transaction fails
func NewCommitTransactionError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to commit transaction",
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewQueryHeadError creates an error when querying head fails
func NewQueryHeadError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to query head",
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewGetVersionsError creates an error when getting versions fails
func NewGetVersionsError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to get versions",
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewHeadVersionNotFoundError creates an error when head version is not found
func NewHeadVersionNotFoundError(headID uint) apierror.Error {
	return newError(
		apierror.NotFound,
		"head version not found",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"head_id": headID}),
		nil,
	)
}

// NewSetHeadError creates an error when setting head fails
func NewSetHeadError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to set head",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewCloseDatabaseError creates an error when closing database fails
func NewCloseDatabaseError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to close database",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewApplyMigrationError creates an error when applying a migration fails
func NewApplyMigrationError(version int, err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to apply migration",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"version": version, "cause": err.Error()}),
		err,
	)
}

// NewUpdateSchemaVersionError creates an error when updating schema version fails
func NewUpdateSchemaVersionError(version int, err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to update schema version",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"version": version, "cause": err.Error()}),
		err,
	)
}

// NewCheckMetadataTableError creates an error when checking metadata table fails
func NewCheckMetadataTableError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to check metadata table",
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewReadSchemaVersionError creates an error when reading schema version fails
func NewReadSchemaVersionError(err error) apierror.Error {
	return newError(
		apierror.Internal,
		"failed to read schema version",
		apierror.True,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewSchemaVersionTooNewError creates an error when schema version is too new
func NewSchemaVersionTooNewError(schemaVersion, currentVersion int) apierror.Error {
	return newError(
		apierror.Internal,
		"database schema version is newer than supported version",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"schema_version": schemaVersion, "supported_version": currentVersion}),
		nil,
	)
}
