package sql

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

func newStoreAlreadyExistsError(id string) error {
	return &StructuredError{
		kind:      apierror.KindAlreadyExists,
		message:   "store " + id + " already exists",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}

func newStoreNotFoundError(id string) error {
	return &StructuredError{
		kind:      apierror.KindNotFound,
		message:   "store " + id + " not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}
