package bootconfig

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrConfigFileRequired = apierror.New(apierror.Invalid, "config file is required").WithRetryable(apierror.False)

	ErrInvalidConfigFormat = apierror.New(apierror.Invalid, "invalid config format").WithRetryable(apierror.False)
)

func NewReadConfigFileError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to read config file").WithCause(cause).WithRetryable(apierror.False)
}

func NewDecodeConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode config").WithCause(cause).WithRetryable(apierror.False)
}

var ErrMissingVersionField = apierror.New(apierror.Invalid, "missing version field").WithRetryable(apierror.False)

func NewParseYAMLError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to parse YAML").WithCause(cause).WithRetryable(apierror.False)
}

func NewUnsupportedVersionError(version string, supported []string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported version: "+version).WithRetryable(apierror.False)
}
