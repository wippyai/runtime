package http

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrEmptyAddr     = apierror.New(apierror.Invalid, "address is required").WithRetryable(apierror.False)
	ErrNilMetadata   = apierror.New(apierror.Invalid, "metadata is required").WithRetryable(apierror.False)
	ErrEmptyFuncName = apierror.New(apierror.Invalid, "function name is required").WithRetryable(apierror.False)
	ErrEmptyPath     = apierror.New(apierror.Invalid, "path is required").WithRetryable(apierror.False)
	ErrEmptyMethod   = apierror.New(apierror.Invalid, "method is required").WithRetryable(apierror.False)
)

func NewMissingMetadataError(field string) apierror.Error {
	return apierror.New(apierror.Invalid, field+" metadata is required").WithRetryable(apierror.False)
}

func NewPathMustStartWithSlashError() apierror.Error {
	return apierror.New(apierror.Invalid, "path must start with /").WithRetryable(apierror.False)
}

func NewInvalidHTTPMethodError(method string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid HTTP method: "+method).WithRetryable(apierror.False)
}

func NewInvalidTimeoutConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid timeout configuration").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidTimeoutError(name string) apierror.Error {
	return apierror.New(apierror.Invalid, name+" must be non-negative").WithRetryable(apierror.False)
}

func NewNegativeConfigError(name string) apierror.Error {
	return apierror.New(apierror.Invalid, name+" must be non-negative").WithRetryable(apierror.False)
}
