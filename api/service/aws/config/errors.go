package config

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error represents an AWS config service error.
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

// NewUnsupportedKindError creates an error for unsupported entry kinds.
func NewUnsupportedKindError(kind string) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported entry kind: " + kind,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"kind": kind}),
	}
}

// NewConfigAlreadyExistsError creates an error when config already exists.
func NewConfigAlreadyExistsError(id string) error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "config " + id + " already exists",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}

// NewDecodeConfigError creates an error for config decode failures.
func NewDecodeConfigError(cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode config",
		retryable: apierror.False,
		cause:     cause,
	}
}

// NewCreateAWSConfigError creates an error for AWS config creation failures.
func NewCreateAWSConfigError(cause error) error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create AWS config",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

// NewConfigNotFoundError creates an error when config is not found.
func NewConfigNotFoundError(id string) error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "config " + id + " not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}
