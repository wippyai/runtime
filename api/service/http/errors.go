package http

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrEmptyAddr     = apierror.New(apierror.Invalid, "address is required").WithRetryable(apierror.False)
	ErrNilMetadata   = apierror.New(apierror.Invalid, "metadata is required").WithRetryable(apierror.False)
	ErrEmptyFuncName = apierror.New(apierror.Invalid, "function name is required").WithRetryable(apierror.False)
	ErrEmptyPath     = apierror.New(apierror.Invalid, "path is required").WithRetryable(apierror.False)
	ErrEmptyMethod   = apierror.New(apierror.Invalid, "method is required").WithRetryable(apierror.False)
)

// NewMissingMetadataError reports missing metadata for a specific field.
func NewMissingMetadataError(field string) apierror.Error {
	return apierror.New(apierror.Invalid, field+" metadata is required").WithRetryable(apierror.False)
}

// NewPathMustStartWithSlashError reports a path validation failure.
func NewPathMustStartWithSlashError() apierror.Error {
	return apierror.New(apierror.Invalid, "path must start with /").WithRetryable(apierror.False)
}

// NewInvalidHTTPMethodError reports an invalid HTTP method.
func NewInvalidHTTPMethodError(method string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid HTTP method: "+method).WithRetryable(apierror.False)
}

// NewInvalidTimeoutConfigError wraps invalid timeout configuration errors.
func NewInvalidTimeoutConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid timeout configuration").WithCause(cause).WithRetryable(apierror.False)
}

// NewInvalidTimeoutError reports a negative timeout value.
func NewInvalidTimeoutError(name string) apierror.Error {
	return apierror.New(apierror.Invalid, name+" must be non-negative").WithRetryable(apierror.False)
}

// NewNegativeConfigError reports a negative configuration value.
func NewNegativeConfigError(name string) apierror.Error {
	return apierror.New(apierror.Invalid, name+" must be non-negative").WithRetryable(apierror.False)
}
