package directory

import apierror "github.com/wippyai/runtime/api/error"

// ErrEmptyDirectoryPath indicates a missing directory path.
var ErrEmptyDirectoryPath = apierror.New(apierror.Invalid, "directory path is required").WithRetryable(apierror.False)

// NewInvalidModeFormatError reports invalid file mode formatting.
func NewInvalidModeFormatError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid file mode format").WithCause(cause).WithRetryable(apierror.False)
}
