package tokenstore

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrStoreIDRequired = apierror.New(apierror.KindInvalid, "store ID is required").WithRetryable(apierror.False)

	ErrTokenLengthMustBePositive = apierror.New(apierror.KindInvalid, "token length must be positive").WithRetryable(apierror.False)

	ErrInvalidTokenStoreConfig = apierror.New(apierror.KindInvalid, "invalid token store config")
)

func NewInvalidDefaultExpirationError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid default_expiration duration format").WithCause(cause).WithRetryable(apierror.False)
}

func NewDecodeTokenStoreConfigError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to decode token store config").WithCause(cause)
}

func NewTokenStoreAlreadyExistsError(id string) apierror.Error {
	return apierror.New(apierror.KindAlreadyExists, "token store "+id+" already exists")
}

func NewTokenStoreNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "token store "+id+" not found")
}

func NewUnsupportedEntryKindError(kind string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "unsupported entry kind: "+kind)
}

func NewCreateTokenStoreError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create token store").WithCause(cause)
}

func NewAcquireBackingStoreError(store string, cause error) apierror.Error {
	return apierror.New(apierror.KindUnavailable, "failed to acquire backing store '"+store+"'").WithCause(cause)
}

func NewGetStoreImplementationError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to get store implementation").WithCause(cause)
}

func NewResourceNotKVStoreError(resource string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "resource '"+resource+"' is not a key-value store")
}

func NewGenerateTokenError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to generate token").WithCause(cause)
}

func NewStoreTokenError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to store token").WithCause(cause)
}

func NewRetrieveTokenError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to retrieve token").WithCause(cause)
}

func NewUnmarshalTokenDataError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to unmarshal token data").WithCause(cause)
}

func NewDeleteTokenError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to delete token").WithCause(cause)
}

func NewGenerateRandomTokenError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to generate random token").WithCause(cause)
}
