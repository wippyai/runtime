package interpolate

import (
	"fmt"

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

var (
	ErrEmptyFilePath = &Error{
		kind:    apierror.KindInvalid,
		message: "empty file path in file:// URL",
	}
)

func NewRelativePathWithoutContextError() *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "cannot resolve relative file path without context filename",
	}
}

func NewPathTraversalError(filePath string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("path traversal detected in file path: %s", filePath),
	}
}

func NewInvalidFilePathError(filePath string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("invalid file path: %s", filePath),
	}
}

func NewReadFileError(filePath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to read file %s", filePath),
		cause:   cause,
	}
}

func NewUnmarshalPayloadError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to unmarshal payload for interpolation",
		cause:   cause,
	}
}

func NewInterpolationError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "interpolation error",
		cause:   cause,
	}
}
