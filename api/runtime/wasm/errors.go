package wasm

import apierror "github.com/wippyai/runtime/api/error"

// Config validation errors.
var (
	ErrSourceRequired = apierror.New(apierror.Invalid, "source is required").
				WithRetryable(apierror.False)

	ErrMethodRequired = apierror.New(apierror.Invalid, "method is required").
				WithRetryable(apierror.False)

	ErrFSRequired = apierror.New(apierror.Invalid, "fs is required").
			WithRetryable(apierror.False)

	ErrPathRequired = apierror.New(apierror.Invalid, "path is required").
			WithRetryable(apierror.False)

	ErrHashRequired = apierror.New(apierror.Invalid, "hash is required").
			WithRetryable(apierror.False)

	ErrTranscoderNotFound = apierror.New(apierror.NotFound, "transcoder not found in context").
				WithRetryable(apierror.False)

	ErrNoHTTPRequest = apierror.New(apierror.Invalid, "no HTTP request in context").
				WithRetryable(apierror.False)
)

// NewInvalidPoolSizeError returns an error for invalid pool size.
func NewInvalidPoolSizeError() apierror.Error {
	return apierror.New(apierror.Invalid, "pool.size must be > 0 for static pools").
		WithRetryable(apierror.False)
}
