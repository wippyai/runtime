package interpolate

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrEmptyFilePath = apierror.New(apierror.Invalid, "empty file path in file:// URL").WithRetryable(apierror.False)
)

func NewRelativePathWithoutContextError() apierror.Error {
	return apierror.New(apierror.Invalid, "cannot resolve relative file path without context filename")
}

func NewPathTraversalError(filePath string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("path traversal detected in file path: %s", filePath))
}

func NewInvalidFilePathError(filePath string) apierror.Error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("invalid file path: %s", filePath))
}

func NewReadFileError(filePath string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, fmt.Sprintf("failed to read file %s", filePath)).WithCause(cause)
}

func NewUnmarshalPayloadError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to unmarshal payload for interpolation").WithCause(cause)
}

func NewInterpolationError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "interpolation error").WithCause(cause)
}
