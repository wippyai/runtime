package sql

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrHostRequired       = apierror.New(apierror.KindInvalid, "host is required").WithRetryable(apierror.False)
	ErrInvalidPort        = apierror.New(apierror.KindInvalid, "port must be greater than 0").WithRetryable(apierror.False)
	ErrDatabaseRequired   = apierror.New(apierror.KindInvalid, "database is required").WithRetryable(apierror.False)
	ErrUsernameRequired   = apierror.New(apierror.KindInvalid, "username is required").WithRetryable(apierror.False)
	ErrPasswordRequired   = apierror.New(apierror.KindInvalid, "password is required").WithRetryable(apierror.False)
	ErrInvalidMaxOpen     = apierror.New(apierror.KindInvalid, "max open connections must be non-negative").WithRetryable(apierror.False)
	ErrInvalidMaxIdle     = apierror.New(apierror.KindInvalid, "max idle connections must be non-negative").WithRetryable(apierror.False)
	ErrInvalidMaxLifetime = apierror.New(apierror.KindInvalid, "max lifetime must be greater than 0").WithRetryable(apierror.False)
	ErrFileRequired       = apierror.New(apierror.KindInvalid, "file path is required").WithRetryable(apierror.False)
)

func NewInvalidDurationError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid duration format").WithCause(cause).WithRetryable(apierror.False)
}
