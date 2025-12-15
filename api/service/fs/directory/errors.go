package directory

import apierror "github.com/wippyai/runtime/api/error"

var ErrEmptyDirectoryPath = apierror.New(apierror.Invalid, "directory path is required").WithRetryable(apierror.False)

func NewInvalidModeFormatError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid file mode format").WithCause(cause).WithRetryable(apierror.False)
}
