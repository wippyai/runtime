package evalhost

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrProcessFactoryNotAvailable = apierror.New(apierror.Internal, "process factory not available").WithRetryable(apierror.False)

	ErrNoResult = apierror.New(apierror.Internal, "process completed with no result").WithRetryable(apierror.False)

	ErrProcessIdle = apierror.New(apierror.Internal, "process became idle").WithRetryable(apierror.False)
)

func NewCompileError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "compile failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewCreateProcessError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create process").WithCause(cause).WithRetryable(apierror.False)
}

func NewCreateProcessFromIDError(id string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create process from ID "+id).WithCause(cause).WithRetryable(apierror.False)
}

func NewModuleNotAvailableError(name string) apierror.Error {
	return apierror.New(apierror.NotFound, "module \""+name+"\" is not available").WithRetryable(apierror.False)
}

func NewParseError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "parse error").WithCause(cause).WithRetryable(apierror.False)
}

func NewCompileScriptError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "compile error").WithCause(cause).WithRetryable(apierror.False)
}

func NewForbiddenClassError(module, class string) apierror.Error {
	return apierror.New(apierror.Invalid, "module \""+module+"\" has forbidden class \""+class+"\"").WithRetryable(apierror.False)
}

func NewRunError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "run failed").WithCause(cause).WithRetryable(apierror.False)
}
