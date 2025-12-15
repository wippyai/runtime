package sql

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrDatabaseIDRequired        = apierror.New(apierror.KindInvalid, "database ID is required").WithRetryable(apierror.False)
	ErrTableNameRequired         = apierror.New(apierror.KindInvalid, "table name is required").WithRetryable(apierror.False)
	ErrIDColumnNameRequired      = apierror.New(apierror.KindInvalid, "ID column name is required").WithRetryable(apierror.False)
	ErrPayloadColumnNameRequired = apierror.New(apierror.KindInvalid, "payload column name is required").WithRetryable(apierror.False)
	ErrExpireColumnNameRequired  = apierror.New(apierror.KindInvalid, "expire column name is required").WithRetryable(apierror.False)
	ErrCleanupIntervalInvalid    = apierror.New(apierror.KindInvalid, "cleanup interval must be non-negative").WithRetryable(apierror.False)
	ErrDatabaseIDInvalid         = apierror.New(apierror.KindInvalid, "database ID contains invalid characters").WithRetryable(apierror.False)
	ErrTableNameInvalid          = apierror.New(apierror.KindInvalid, "table name contains invalid characters").WithRetryable(apierror.False)
	ErrIDColumnNameInvalid       = apierror.New(apierror.KindInvalid, "ID column name contains invalid characters").WithRetryable(apierror.False)
	ErrPayloadColumnNameInvalid  = apierror.New(apierror.KindInvalid, "payload column name contains invalid characters").WithRetryable(apierror.False)
	ErrExpireColumnNameInvalid   = apierror.New(apierror.KindInvalid, "expire column name contains invalid characters").WithRetryable(apierror.False)
)

func NewInvalidCleanupIntervalError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid cleanup interval duration").WithCause(cause).WithRetryable(apierror.False)
}
