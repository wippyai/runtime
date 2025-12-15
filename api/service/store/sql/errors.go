package sql

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrDatabaseIDRequired        = apierror.New(apierror.Invalid, "database ID is required").WithRetryable(apierror.False)
	ErrTableNameRequired         = apierror.New(apierror.Invalid, "table name is required").WithRetryable(apierror.False)
	ErrIDColumnNameRequired      = apierror.New(apierror.Invalid, "ID column name is required").WithRetryable(apierror.False)
	ErrPayloadColumnNameRequired = apierror.New(apierror.Invalid, "payload column name is required").WithRetryable(apierror.False)
	ErrExpireColumnNameRequired  = apierror.New(apierror.Invalid, "expire column name is required").WithRetryable(apierror.False)
	ErrCleanupIntervalInvalid    = apierror.New(apierror.Invalid, "cleanup interval must be non-negative").WithRetryable(apierror.False)
	ErrDatabaseIDInvalid         = apierror.New(apierror.Invalid, "database ID contains invalid characters").WithRetryable(apierror.False)
	ErrTableNameInvalid          = apierror.New(apierror.Invalid, "table name contains invalid characters").WithRetryable(apierror.False)
	ErrIDColumnNameInvalid       = apierror.New(apierror.Invalid, "ID column name contains invalid characters").WithRetryable(apierror.False)
	ErrPayloadColumnNameInvalid  = apierror.New(apierror.Invalid, "payload column name contains invalid characters").WithRetryable(apierror.False)
	ErrExpireColumnNameInvalid   = apierror.New(apierror.Invalid, "expire column name contains invalid characters").WithRetryable(apierror.False)
)
