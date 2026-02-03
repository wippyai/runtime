package store

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrStoreFull   = apierror.New(apierror.Unavailable, "store is full").WithRetryable(apierror.True)
	ErrStoreClosed = apierror.New(apierror.Unavailable, "store is closed").WithRetryable(apierror.False)
)

// NewUnsupportedKindError creates an unsupported kind error.
func NewUnsupportedKindError(kind string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported entry kind").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

// NewStoreAlreadyExistsError creates a store already exists error.
func NewStoreAlreadyExistsError(id string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "store already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

// NewStoreNotFoundError creates a store not found error.
func NewStoreNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "store not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}
