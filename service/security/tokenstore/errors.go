package tokenstore

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrInvalidTokenStoreConfig = apierror.New(apierror.Invalid, "invalid token store config").WithRetryable(apierror.False)
)

func NewDecodeTokenStoreConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode token store config").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func NewTokenStoreAlreadyExistsError(id string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "token store already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewTokenStoreNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "token store not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewUnsupportedEntryKindError(kind string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported entry kind").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewCreateTokenStoreError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create token store").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func NewAcquireBackingStoreError(store string, cause error) apierror.Error {
	return apierror.New(apierror.Unavailable, "failed to acquire backing store").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"store": store,
			"cause": cause.Error(),
		})).
		WithCause(cause)
}

func NewGetStoreImplementationError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to get store implementation").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func NewResourceNotKVStoreError(resource string) apierror.Error {
	return apierror.New(apierror.Invalid, "resource is not a key-value store").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"resource": resource}))
}

func NewGenerateTokenError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to generate token").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func NewStoreTokenError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to store token").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func NewRetrieveTokenError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to retrieve token").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func NewUnmarshalTokenDataError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to unmarshal token data").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func NewDeleteTokenError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to delete token").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func NewGenerateRandomTokenError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to generate random token").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}
