package evalhost

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrProcessFactoryNotAvailable = apierror.New(apierror.KindInternal, "process factory not available").WithRetryable(apierror.False)

	ErrNoResult = apierror.New(apierror.KindInternal, "process completed with no result").WithRetryable(apierror.False)

	ErrProcessIdle = apierror.New(apierror.KindInternal, "process became idle").WithRetryable(apierror.False)
)

func NewCompileError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "compile failed").WithCause(cause).WithRetryable(apierror.False)
}

func NewCreateProcessError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create process").WithCause(cause).WithRetryable(apierror.False)
}

func NewCreateProcessFromIDError(id string, cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create process from ID "+id).WithCause(cause).WithRetryable(apierror.False)
}

func NewModuleNotAvailableError(name string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "module \""+name+"\" is not available").WithRetryable(apierror.False)
}

func NewParseError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "parse error").WithCause(cause).WithRetryable(apierror.False)
}

func NewCompileScriptError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "compile error").WithCause(cause).WithRetryable(apierror.False)
}

func NewForbiddenClassError(module, class string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "module \""+module+"\" has forbidden class \""+class+"\"").WithRetryable(apierror.False)
}

func NewRunError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "run failed").WithCause(cause).WithRetryable(apierror.False)
}
