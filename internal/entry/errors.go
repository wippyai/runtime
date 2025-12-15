package entry

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrKindRequired = apierror.New(apierror.KindInvalid, "kind is required").WithRetryable(apierror.False)

	ErrIDRequired = apierror.New(apierror.KindInvalid, "id is required").WithRetryable(apierror.False)

	ErrConfigRequired = apierror.New(apierror.KindInvalid, "config is required").WithRetryable(apierror.False)

	ErrConfigurationDataRequired = apierror.New(apierror.KindInvalid, "configuration data is required").WithRetryable(apierror.False)

	ErrEmptyPath = apierror.New(apierror.KindInvalid, "path cannot be empty").WithRetryable(apierror.False)

	ErrCannotReplaceEntireDataField = apierror.New(apierror.KindInvalid, "cannot replace entire data field").WithRetryable(apierror.False)

	ErrCannotReplaceEntireMetaField = apierror.New(apierror.KindInvalid, "cannot replace entire meta field").WithRetryable(apierror.False)

	ErrEmptyPathSegments = apierror.New(apierror.KindInvalid, "path segments cannot be empty").WithRetryable(apierror.False)
)

func NewUnmarshalConfigError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to unmarshal config").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidConfigurationError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid configuration").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidTargetError(target string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid target: "+target).WithRetryable(apierror.False)
}

var (
	ErrCannotAppendToEntireDataField = apierror.New(apierror.KindInvalid, "cannot append to entire data field").WithRetryable(apierror.False)

	ErrCannotAppendToEntireMetaField = apierror.New(apierror.KindInvalid, "cannot append to entire meta field").WithRetryable(apierror.False)

	ErrCannotDeleteEntireDataField = apierror.New(apierror.KindInvalid, "cannot delete entire data field").WithRetryable(apierror.False)

	ErrCannotDeleteEntireMetaField = apierror.New(apierror.KindInvalid, "cannot delete entire meta field").WithRetryable(apierror.False)
)

func NewTranscodeToGolangError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to transcode to golang").WithCause(cause).WithRetryable(apierror.False)
}

func NewCannotAppendToNonArrayError(path string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "cannot append to non-array at path: "+path).WithRetryable(apierror.False)
}
