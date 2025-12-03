package memory

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
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

func errUnsupportedKind(kind string) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported entry kind",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"kind": kind}),
	}
}

func errDecodeConfig(cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode configuration",
		retryable: apierror.False,
		cause:     cause,
	}
}

func errInvalidConfig(cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid configuration",
		retryable: apierror.False,
		cause:     cause,
	}
}

func errStorageNotExists(id registry.ID) error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "storage does not exist",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"storage_id": id.String()}),
	}
}
