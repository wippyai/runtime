package tokenstore

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrInvalidTokenStoreConfig = apierror.New(apierror.Invalid, "invalid token store config")
)

func NewDecodeTokenStoreConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode token store config").WithCause(cause)
}

func NewTokenStoreAlreadyExistsError(id string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "token store "+id+" already exists")
}

func NewTokenStoreNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "token store "+id+" not found")
}

func NewUnsupportedEntryKindError(kind string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported entry kind: "+kind)
}

func NewCreateTokenStoreError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create token store").WithCause(cause)
}

func NewAcquireBackingStoreError(store string, cause error) apierror.Error {
	return apierror.New(apierror.Unavailable, "failed to acquire backing store '"+store+"'").WithCause(cause)
}

func NewGetStoreImplementationError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get store implementation").WithCause(cause)
}

func NewResourceNotKVStoreError(resource string) apierror.Error {
	return apierror.New(apierror.Invalid, "resource '"+resource+"' is not a key-value store")
}

func NewGenerateTokenError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to generate token").WithCause(cause)
}

func NewStoreTokenError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to store token").WithCause(cause)
}

func NewRetrieveTokenError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to retrieve token").WithCause(cause)
}

func NewUnmarshalTokenDataError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to unmarshal token data").WithCause(cause)
}

func NewDeleteTokenError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to delete token").WithCause(cause)
}

func NewGenerateRandomTokenError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to generate random token").WithCause(cause)
}
