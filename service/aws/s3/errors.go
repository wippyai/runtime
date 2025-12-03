package s3

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

type StructuredError struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *StructuredError) Error() string               { return e.message }
func (e *StructuredError) Kind() apierror.Kind         { return e.kind }
func (e *StructuredError) Retryable() apierror.Ternary { return e.retryable }
func (e *StructuredError) Details() attrs.Attributes   { return e.details }
func (e *StructuredError) Unwrap() error               { return e.cause }

func newUnsupportedKindError(kind string) error {
	return &StructuredError{
		kind:      apierror.KindInvalid,
		message:   "unsupported entry kind: " + kind,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"kind": kind}),
	}
}

func newStorageAlreadyExistsError(id string) error {
	return &StructuredError{
		kind:      apierror.KindAlreadyExists,
		message:   "storage " + id + " already exists",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}

func newAddEntryError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInternal,
		message:   "add entry",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newStorageNotFoundError(id string) error {
	return &StructuredError{
		kind:      apierror.KindNotFound,
		message:   "storage " + id + " not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}

func newUpdateEntryError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInternal,
		message:   "update entry",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newDecodeConfigError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInvalid,
		message:   "decode config",
		retryable: apierror.False,
		cause:     cause,
	}
}

func newAcquireResourceError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInternal,
		message:   "acquire resource",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newGetConfigError(cause error) error {
	return &StructuredError{
		kind:      apierror.KindInternal,
		message:   "get config",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newAWSConfigNotConfigError() error {
	return &StructuredError{
		kind:      apierror.KindInternal,
		message:   "aws config not config",
		retryable: apierror.False,
	}
}
