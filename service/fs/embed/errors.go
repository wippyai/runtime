package embed

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

func newUnsupportedEntryKindError(kind string) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported entry kind: " + kind,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"kind": kind}),
	}
}

func newFailedToDecodeConfigError(cause error) error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode config",
		retryable: apierror.False,
		cause:     cause,
	}
}

func newEmbeddedFilesystemAlreadyExistsError(id string) error {
	return &Error{
		kind:      apierror.KindAlreadyExists,
		message:   "embedded filesystem " + id + " already exists",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}

func newEmbeddedFilesystemNotFoundError(id string) error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "embedded filesystem " + id + " not found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
	}
}

func newFailedToGetEmbeddedFilesystemError(cause error) error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to get embedded filesystem",
		retryable: apierror.Unknown,
		cause:     cause,
	}
}

func newPackPathCannotBeEmptyError() error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "packPath cannot be empty",
		retryable: apierror.False,
	}
}

func newReaderCannotBeNilError() error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "reader cannot be nil",
		retryable: apierror.False,
	}
}

func newEmbeddedFilesystemNotFoundInRegistryError(id string, cause error) error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "embedded filesystem not found: " + id,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"id": id}),
		cause:     cause,
	}
}
