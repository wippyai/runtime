package tokenstore

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrStoreIDRequired           = apierror.New(apierror.KindInvalid, "store ID is required").WithRetryable(apierror.False)
	ErrTokenLengthMustBePositive = apierror.New(apierror.KindInvalid, "token length must be positive").WithRetryable(apierror.False)
)

func NewInvalidDefaultExpirationError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid default expiration duration").WithCause(cause).WithRetryable(apierror.False)
}
