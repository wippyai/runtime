package storage

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrBasePathEmpty = apierror.New(apierror.KindInvalid, "basePath cannot be empty").WithRetryable(apierror.False)
)

func NewCleanOldVersionError(basePath string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, fmt.Sprintf("failed to clean old version at %q", basePath)).WithCause(cause)
}

func NewCreateBaseDirectoryError(basePath string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, fmt.Sprintf("failed to create base directory %q", basePath)).WithCause(cause)
}

func NewFileNilError(index int) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("file at index %d is nil", index))
}

func NewFileEmptyPathError(index int) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("file at index %d has empty path", index))
}

func NewFileEmptyPathAfterStripError(index int) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("file at index %d has empty path after stripping legacy prefix", index))
}

func NewFileAbsolutePathError(index int, filePath string) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("file at index %d has absolute path %q, expected relative path", index, filePath))
}

func NewFileInvalidPathError(index int, filePath string) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("file at index %d has invalid path %q", index, filePath))
}

func NewCreateDirectoryError(filePath string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, fmt.Sprintf("failed to create directory for %q", filePath)).WithCause(cause)
}

func NewWriteFileError(filePath string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, fmt.Sprintf("failed to write file %q", filePath)).WithCause(cause)
}

func NewDirectoryNotExistError(basePath string) apierror.Error {
	return apierror.New(apierror.KindNotFound, fmt.Sprintf("directory %q does not exist", basePath))
}

func NewStatDirectoryError(basePath string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, fmt.Sprintf("stat directory %q", basePath)).WithCause(cause)
}

func NewPathNotDirectoryError(basePath string) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("path %q is not a directory", basePath))
}

func NewReadDirectoryError(basePath string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, fmt.Sprintf("read directory %q", basePath)).WithCause(cause)
}

func NewRefuseDeleteProtectedPathError(basePath string) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("refusing to delete protected path %q", basePath))
}

func NewRemoveDirectoryError(basePath string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, fmt.Sprintf("remove directory %q", basePath)).WithCause(cause)
}

func NewLoadEntriesError(basePath string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, fmt.Sprintf("load entries from %s", basePath)).WithCause(cause)
}

func NewComputeHashError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "compute hash").WithCause(cause)
}
