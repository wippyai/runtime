package sql

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrHostRequired       = apierror.New(apierror.Invalid, "host is required").WithRetryable(apierror.False)
	ErrInvalidPort        = apierror.New(apierror.Invalid, "port must be greater than 0").WithRetryable(apierror.False)
	ErrDatabaseRequired   = apierror.New(apierror.Invalid, "database is required").WithRetryable(apierror.False)
	ErrUsernameRequired   = apierror.New(apierror.Invalid, "username is required").WithRetryable(apierror.False)
	ErrPasswordRequired   = apierror.New(apierror.Invalid, "password is required").WithRetryable(apierror.False)
	ErrInvalidMaxOpen     = apierror.New(apierror.Invalid, "max open connections must be non-negative").WithRetryable(apierror.False)
	ErrInvalidMaxIdle     = apierror.New(apierror.Invalid, "max idle connections must be non-negative").WithRetryable(apierror.False)
	ErrInvalidMaxLifetime = apierror.New(apierror.Invalid, "max lifetime must be greater than 0").WithRetryable(apierror.False)
	ErrFileRequired       = apierror.New(apierror.Invalid, "file path is required").WithRetryable(apierror.False)
)
