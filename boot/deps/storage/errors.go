package storage

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
	ErrBasePathEmpty = &Error{
		kind:    apierror.KindInvalid,
		message: "basePath cannot be empty",
	}
)

func NewCleanOldVersionError(basePath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to clean old version at %q", basePath),
		cause:   cause,
	}
}

func NewCreateBaseDirectoryError(basePath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to create base directory %q", basePath),
		cause:   cause,
	}
}

func NewFileNilError(index int) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("file at index %d is nil", index),
	}
}

func NewFileEmptyPathError(index int) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("file at index %d has empty path", index),
	}
}

func NewFileEmptyPathAfterStripError(index int) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("file at index %d has empty path after stripping legacy prefix", index),
	}
}

func NewFileAbsolutePathError(index int, filePath string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("file at index %d has absolute path %q, expected relative path", index, filePath),
	}
}

func NewFileInvalidPathError(index int, filePath string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("file at index %d has invalid path %q", index, filePath),
	}
}

func NewCreateDirectoryError(filePath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to create directory for %q", filePath),
		cause:   cause,
	}
}

func NewWriteFileError(filePath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("failed to write file %q", filePath),
		cause:   cause,
	}
}

func NewDirectoryNotExistError(basePath string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: fmt.Sprintf("directory %q does not exist", basePath),
	}
}

func NewStatDirectoryError(basePath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("stat directory %q", basePath),
		cause:   cause,
	}
}

func NewPathNotDirectoryError(basePath string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("path %q is not a directory", basePath),
	}
}

func NewReadDirectoryError(basePath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("read directory %q", basePath),
		cause:   cause,
	}
}

func NewRefuseDeleteProtectedPathError(basePath string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: fmt.Sprintf("refusing to delete protected path %q", basePath),
	}
}

func NewRemoveDirectoryError(basePath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("remove directory %q", basePath),
		cause:   cause,
	}
}

func NewLoadEntriesError(basePath string, cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: fmt.Sprintf("load entries from %s", basePath),
		cause:   cause,
	}
}

func NewComputeHashError(cause error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "compute hash",
		cause:   cause,
	}
}
