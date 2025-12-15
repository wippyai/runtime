package store

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

var (
	ErrStoreFull   = apierror.New(apierror.Unavailable, "store is full").WithRetryable(apierror.True)
	ErrStoreClosed = apierror.New(apierror.Unavailable, "store is closed").WithRetryable(apierror.False)
)

// NewKeyNotFoundError creates a key not found error with details.
func NewKeyNotFoundError(key registry.ID) apierror.Error {
	return apierror.E(
		apierror.NotFound,
		"key not found",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"key": key.String()}),
		nil,
	)
}

// NewKeyExistsError creates a key exists error with details.
func NewKeyExistsError(key registry.ID) apierror.Error {
	return apierror.E(
		apierror.AlreadyExists,
		"key already exists",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"key": key.String()}),
		nil,
	)
}

// NewInvalidKeyError creates an invalid key error with details.
func NewInvalidKeyError(key string, reason string) apierror.Error {
	return apierror.E(
		apierror.Invalid,
		"invalid key format: "+reason,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"key": key, "reason": reason}),
		nil,
	)
}

// NewUnsupportedKindError creates an unsupported kind error.
func NewUnsupportedKindError(kind string) apierror.Error {
	return apierror.E(
		apierror.Invalid,
		"unsupported entry kind: "+kind,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"kind": kind}),
		nil,
	)
}

// NewStoreAlreadyExistsError creates a store already exists error.
func NewStoreAlreadyExistsError(id string) apierror.Error {
	return apierror.E(
		apierror.AlreadyExists,
		"store "+id+" already exists",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"id": id}),
		nil,
	)
}

// NewStoreNotFoundError creates a store not found error.
func NewStoreNotFoundError(id string) apierror.Error {
	return apierror.E(
		apierror.NotFound,
		"store "+id+" not found",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"id": id}),
		nil,
	)
}
