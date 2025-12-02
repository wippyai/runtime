package sqlite

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error implements apierror.Error for sqlite history errors
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

// NewOpenDatabaseError creates an error when opening database fails
func NewOpenDatabaseError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to open database: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewConnectError creates an error when connecting to database fails
func NewConnectError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to connect to database: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewMigrationError creates an error when running migrations fails
func NewMigrationError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to run migrations: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewEnsureRootVersionError creates an error when ensuring root version fails
func NewEnsureRootVersionError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to ensure root version: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewCheckRootVersionError creates an error when checking root version fails
func NewCheckRootVersionError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to check root version: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewInsertRootVersionError creates an error when inserting root version fails
func NewInsertRootVersionError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to insert root version: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewInsertChangesetError creates an error when inserting changeset fails
func NewInsertChangesetError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to insert changeset: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewSetInitialHeadError creates an error when setting initial head fails
func NewSetInitialHeadError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to set initial head: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewQueryVersionsError creates an error when querying versions fails
func NewQueryVersionsError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to query versions: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewScanVersionError creates an error when scanning version row fails
func NewScanVersionError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to scan version: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewInvalidParentVersionError creates an error for invalid parent version ID
func NewInvalidParentVersionError(parentID int64) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "invalid negative parent version ID",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"parent_id": parentID}),
	}
}

// NewParentVersionNotFoundError creates an error when parent version is not found
func NewParentVersionNotFoundError(parentID, versionID uint) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "parent version not found for version",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"parent_id": parentID, "version_id": versionID}),
	}
}

// NewIterateVersionsError creates an error when iterating versions fails
func NewIterateVersionsError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "error iterating versions: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewChangesetNotFoundError creates an error when changeset is not found
func NewChangesetNotFoundError(versionID uint) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "changeset not found for version",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"version_id": versionID}),
	}
}

// NewQueryChangesetError creates an error when querying changeset fails
func NewQueryChangesetError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to query changeset: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewDecodeChangesetError creates an error when decoding changeset fails
func NewDecodeChangesetError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to decode changeset: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewBeginTransactionError creates an error when beginning transaction fails
func NewBeginTransactionError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to begin transaction: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewParentVersionIDTooLargeError creates an error when parent version ID is too large
func NewParentVersionIDTooLargeError(prevID uint) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "parent version ID too large",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"parent_id": prevID}),
	}
}

// NewInsertVersionError creates an error when inserting version fails
func NewInsertVersionError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to insert version: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewEncodeChangesetError creates an error when encoding changeset fails
func NewEncodeChangesetError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to encode changeset: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewUpdateHeadError creates an error when updating head fails
func NewUpdateHeadError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to update head: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewCommitTransactionError creates an error when committing transaction fails
func NewCommitTransactionError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to commit transaction: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewQueryHeadError creates an error when querying head fails
func NewQueryHeadError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to query head: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewGetVersionsError creates an error when getting versions fails
func NewGetVersionsError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to get versions: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewHeadVersionNotFoundError creates an error when head version is not found
func NewHeadVersionNotFoundError(headID uint) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "head version not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"head_id": headID}),
	}
}

// NewSetHeadError creates an error when setting head fails
func NewSetHeadError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to set head: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewCloseDatabaseError creates an error when closing database fails
func NewCloseDatabaseError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to close database: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewApplyMigrationError creates an error when applying a migration fails
func NewApplyMigrationError(version int, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to apply migration: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"version": version, "cause": err.Error()}),
		cause:     err,
	}
}

// NewUpdateSchemaVersionError creates an error when updating schema version fails
func NewUpdateSchemaVersionError(version int, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to update schema version: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"version": version, "cause": err.Error()}),
		cause:     err,
	}
}

// NewCheckMetadataTableError creates an error when checking metadata table fails
func NewCheckMetadataTableError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to check metadata table: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewReadSchemaVersionError creates an error when reading schema version fails
func NewReadSchemaVersionError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to read schema version: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewSchemaVersionTooNewError creates an error when schema version is too new
func NewSchemaVersionTooNewError(schemaVersion, currentVersion int) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "database schema version is newer than supported version",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"schema_version": schemaVersion, "supported_version": currentVersion}),
	}
}
