package interpolate

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrEmptyFilePath = apierror.New(apierror.KindInvalid, "empty file path in file:// URL").WithRetryable(apierror.False)
)

func NewRelativePathWithoutContextError() apierror.Error {
	return apierror.New(apierror.KindInvalid, "cannot resolve relative file path without context filename")
}

func NewPathTraversalError(filePath string) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("path traversal detected in file path: %s", filePath))
}

func NewInvalidFilePathError(filePath string) apierror.Error {
	return apierror.New(apierror.KindInvalid, fmt.Sprintf("invalid file path: %s", filePath))
}

func NewReadFileError(filePath string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, fmt.Sprintf("failed to read file %s", filePath)).WithCause(cause)
}

func NewUnmarshalPayloadError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to unmarshal payload for interpolation").WithCause(cause)
}

func NewInterpolationError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "interpolation error").WithCause(cause)
}
