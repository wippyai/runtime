package s3

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error represents an S3 service error.
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

// NewStorageAlreadyExistsError creates an error when storage already exists.
func NewStorageAlreadyExistsError(id string) error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "storage " + id + " already exists",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}

// NewAddEntryError creates an error for add entry failures.
func NewAddEntryError(cause error) error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to add entry",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

// NewStorageNotFoundError creates an error when storage is not found.
func NewStorageNotFoundError(id string) error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "storage " + id + " not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}

// NewUpdateEntryError creates an error for update entry failures.
func NewUpdateEntryError(cause error) error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to update entry",
		retryable: apierror.Unknown,
		cause:     cause,
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

// NewAcquireResourceError creates an error for resource acquisition failures.
func NewAcquireResourceError(cause error) error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to acquire resource",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

// NewGetConfigError creates an error for config retrieval failures.
func NewGetConfigError(cause error) error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to get config",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

// NewAWSConfigInvalidError creates an error when AWS config resource is not a valid config.
func NewAWSConfigInvalidError() error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "aws config resource is not a valid config",
		retryable: apierror.False,
	}
}
