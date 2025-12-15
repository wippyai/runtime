package http

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrEmptyAddr     = apierror.New(apierror.KindInvalid, "address is required").WithRetryable(apierror.False)
	ErrNilMetadata   = apierror.New(apierror.KindInvalid, "metadata is required").WithRetryable(apierror.False)
	ErrEmptyFuncName = apierror.New(apierror.KindInvalid, "function name is required").WithRetryable(apierror.False)
	ErrEmptyPath     = apierror.New(apierror.KindInvalid, "path is required").WithRetryable(apierror.False)
	ErrEmptyMethod   = apierror.New(apierror.KindInvalid, "method is required").WithRetryable(apierror.False)
)

func NewMissingMetadataError(field string) apierror.Error {
	return apierror.New(apierror.KindInvalid, field+" metadata is required").WithRetryable(apierror.False)
}

func NewPathMustStartWithSlashError() apierror.Error {
	return apierror.New(apierror.KindInvalid, "path must start with /").WithRetryable(apierror.False)
}

func NewInvalidHTTPMethodError(method string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid HTTP method: "+method).WithRetryable(apierror.False)
}

func NewInvalidDurationError(field string, cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid duration for "+field).WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidTimeoutConfigError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid timeout configuration").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidTimeoutError(name string) apierror.Error {
	return apierror.New(apierror.KindInvalid, name+" must be non-negative").WithRetryable(apierror.False)
}

func NewNegativeConfigError(name string) apierror.Error {
	return apierror.New(apierror.KindInvalid, name+" must be non-negative").WithRetryable(apierror.False)
}
