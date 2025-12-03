package loader

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

func NewUnsupportedFileFormatError(path string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("unsupported file format for file %s", path),
	}
}

func NewWalkFilesystemError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to walk filesystem",
		cause:   cause,
	}
}

func NewWalkDirectoryError(dirPath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to walk directory %s", dirPath),
		cause:   cause,
	}
}

func NewOpenFileError(path string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to open file %s", path),
		cause:   cause,
	}
}

func NewReadFileError(path string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to read file %s", path),
		cause:   cause,
	}
}

func NewUnsupportedFormatError(format string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("unsupported file format: %s", format),
	}
}

func NewUnmarshalContentError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "failed to unmarshal content",
		cause:   cause,
	}
}

func NewValidationFailedError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "validation failed",
		cause:   cause,
	}
}

func NewLoadFilesError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to load files",
		cause:   cause,
	}
}

func NewLoadDirectoryFilesError(dirPath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to load files from directory %s", dirPath),
		cause:   cause,
	}
}

func NewLoadFileError(filePath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to load file %s", filePath),
		cause:   cause,
	}
}

func NewProcessFileError(filePath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to process file %s", filePath),
		cause:   cause,
	}
}

func NewInterpolationError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "interpolation failed",
		cause:   cause,
	}
}

func NewExtractEntriesError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to extract entries",
		cause:   cause,
	}
}

func NewInvalidEntryError(source string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("invalid entry in %s", source),
		cause:   cause,
	}
}

var (
	ErrMissingNamespace = &Error{
		kind:    apierror.KindInvalid,
		message: "missing namespace",
	}
	ErrMissingName = &Error{
		kind:    apierror.KindInvalid,
		message: "missing name",
	}
	ErrMissingKind = &Error{
		kind:    apierror.KindInvalid,
		message: "missing kind",
	}
)
