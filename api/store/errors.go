package store

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Sentinel errors.
var (
	ErrKeyNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "key not found",
		retryable: apierror.False,
	}

	ErrKeyExists = &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "key already exists",
		retryable: apierror.False,
	}

	ErrInvalidKey = &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid key format",
		retryable: apierror.False,
	}

	ErrStoreFull = &Error{
		kind:      apierror.KindUnavailable,
		message:   "store is full",
		retryable: apierror.True,
	}

	ErrStoreClosed = &Error{
		kind:      apierror.KindUnavailable,
		message:   "store is closed",
		retryable: apierror.False,
	}
)

// Error implements apierror.Error for store errors.
type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }

// NewKeyNotFoundError creates a key not found error with details.
func NewKeyNotFoundError(key registry.ID) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "key not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"key": key.String()}),
	}
}

// NewKeyExistsError creates a key exists error with details.
func NewKeyExistsError(key registry.ID) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "key already exists",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"key": key.String()}),
	}
}

// NewInvalidKeyError creates an invalid key error with details.
func NewInvalidKeyError(key string, reason string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid key format: " + reason,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"key": key, "reason": reason}),
	}
}
