package config

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

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

func newUnsupportedKindError(kind string) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported entry kind: " + kind,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"kind": kind}),
	}
}

func newStorageAlreadyExistsError(id string) error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "storage " + id + " already exists",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}

func newDecodeConfigError(cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "decode config",
		retryable: apierror.False,
		cause:     cause,
	}
}

func newCreateAWSConfigError(cause error) error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "create AWS config",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newStorageNotFoundError(id string) error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "storage " + id + " not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}

func newConfigNotFoundError(id string) error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "config " + id + " not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}
