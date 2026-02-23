// SPDX-License-Identifier: MPL-2.0

package loader

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrMissingNamespace = apierror.New(apierror.Invalid, "missing namespace").WithRetryable(apierror.False)
	ErrMissingName      = apierror.New(apierror.Invalid, "missing name").WithRetryable(apierror.False)
	ErrMissingKind      = apierror.New(apierror.Invalid, "missing kind").WithRetryable(apierror.False)
)

func NewUnsupportedFileFormatError(path string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("unsupported file format for file %s", path))
}

func NewWalkFilesystemError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to walk filesystem").WithCause(cause)
}

func NewWalkDirectoryError(dirPath string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to walk directory %s", dirPath)).WithCause(cause)
}

func NewOpenFileError(path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to open file %s", path)).WithCause(cause)
}

func NewReadFileError(path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to read file %s", path)).WithCause(cause)
}

func NewUnsupportedFormatError(format string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("unsupported file format: %s", format))
}

func NewUnmarshalContentError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to unmarshal content").WithCause(cause)
}

func NewValidationFailedError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "validation failed").WithCause(cause)
}

func NewLoadFilesError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to load files").WithCause(cause)
}

func NewLoadDirectoryFilesError(dirPath string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to load files from directory %s", dirPath)).WithCause(cause)
}

func NewLoadFileError(filePath string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to load file %s", filePath)).WithCause(cause)
}

func NewProcessFileError(filePath string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to process file %s", filePath)).WithCause(cause)
}

func NewInterpolationError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "interpolation failed").WithCause(cause)
}

func NewExtractEntriesError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to extract entries").WithCause(cause)
}

func NewInvalidEntryError(source string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("invalid entry in %s", source)).WithCause(cause)
}
