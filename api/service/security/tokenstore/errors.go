package tokenstore

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrStoreIDRequired           = apierror.New(apierror.Invalid, "store ID is required").WithRetryable(apierror.False)
	ErrTokenLengthMustBePositive = apierror.New(apierror.Invalid, "token length must be positive").WithRetryable(apierror.False)
)

func NewInvalidDefaultExpirationError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid default expiration duration").WithCause(cause).WithRetryable(apierror.False)
}
