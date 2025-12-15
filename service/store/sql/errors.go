package sql

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrDatabaseIDRequired = apierror.New(apierror.KindInvalid, "database ID is required").WithRetryable(apierror.False)

	ErrTableNameRequired = apierror.New(apierror.KindInvalid, "table_name is required").WithRetryable(apierror.False)

	ErrIDColumnNameRequired = apierror.New(apierror.KindInvalid, "id_column_name is required").WithRetryable(apierror.False)

	ErrPayloadColumnNameRequired = apierror.New(apierror.KindInvalid, "payload_column_name is required").WithRetryable(apierror.False)

	ErrExpireColumnNameRequired = apierror.New(apierror.KindInvalid, "expire_column_name is required").WithRetryable(apierror.False)

	ErrCleanupIntervalInvalid = apierror.New(apierror.KindInvalid, "cleanup_interval must be greater than or equal to 0").WithRetryable(apierror.False)

	ErrDatabaseIDInvalid = apierror.New(apierror.KindInvalid, "database ID is invalid").WithRetryable(apierror.False)

	ErrTableNameInvalid = apierror.New(apierror.KindInvalid, "table_name is invalid").WithRetryable(apierror.False)

	ErrIDColumnNameInvalid = apierror.New(apierror.KindInvalid, "id_column_name is invalid").WithRetryable(apierror.False)

	ErrPayloadColumnNameInvalid = apierror.New(apierror.KindInvalid, "payload_column_name is invalid").WithRetryable(apierror.False)

	ErrExpireColumnNameInvalid = apierror.New(apierror.KindInvalid, "expire_column_name is invalid").WithRetryable(apierror.False)

	ErrInvalidResourceType = apierror.New(apierror.KindInvalid, "acquired resource is not a valid database connection").WithRetryable(apierror.False)
)

func NewInvalidCleanupIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid CleanupInterval duration format").WithCause(cause).WithRetryable(apierror.False)
}
