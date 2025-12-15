package sql

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrDatabaseIDRequired = apierror.New(apierror.Invalid, "database ID is required").WithRetryable(apierror.False)

	ErrTableNameRequired = apierror.New(apierror.Invalid, "table_name is required").WithRetryable(apierror.False)

	ErrIDColumnNameRequired = apierror.New(apierror.Invalid, "id_column_name is required").WithRetryable(apierror.False)

	ErrPayloadColumnNameRequired = apierror.New(apierror.Invalid, "payload_column_name is required").WithRetryable(apierror.False)

	ErrExpireColumnNameRequired = apierror.New(apierror.Invalid, "expire_column_name is required").WithRetryable(apierror.False)

	ErrCleanupIntervalInvalid = apierror.New(apierror.Invalid, "cleanup_interval must be greater than or equal to 0").WithRetryable(apierror.False)

	ErrDatabaseIDInvalid = apierror.New(apierror.Invalid, "database ID is invalid").WithRetryable(apierror.False)

	ErrTableNameInvalid = apierror.New(apierror.Invalid, "table_name is invalid").WithRetryable(apierror.False)

	ErrIDColumnNameInvalid = apierror.New(apierror.Invalid, "id_column_name is invalid").WithRetryable(apierror.False)

	ErrPayloadColumnNameInvalid = apierror.New(apierror.Invalid, "payload_column_name is invalid").WithRetryable(apierror.False)

	ErrExpireColumnNameInvalid = apierror.New(apierror.Invalid, "expire_column_name is invalid").WithRetryable(apierror.False)

	ErrInvalidResourceType = apierror.New(apierror.Invalid, "acquired resource is not a valid database connection").WithRetryable(apierror.False)
)

func NewInvalidCleanupIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid CleanupInterval duration format").WithCause(cause).WithRetryable(apierror.False)
}
