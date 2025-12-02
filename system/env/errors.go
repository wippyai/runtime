package env

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Error implements apierror.Error for env registry errors
type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

// NewSubscriberError creates an error for subscriber creation failure
func NewSubscriberError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create subscriber: " + err.Error(),
		retryable: apierror.True,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewInvalidVariableError creates an error for invalid variable
func NewInvalidVariableError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid variable: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewVariableNameExistsError creates an error when variable name already exists
func NewVariableNameExistsError(name string) *Error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "variable name already exists: " + name,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"name": name}),
	}
}

// NewInvalidStorageTypeError creates an error for invalid storage type
func NewInvalidStorageTypeError(id registry.ID) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "invalid storage type for " + id.String(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id.String()}),
	}
}
