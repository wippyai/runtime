package eval

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrCodeRequired = apierror.New(apierror.KindInvalid, "code is required").WithRetryable(apierror.False)
)

func NewCompileError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "compile error").WithCause(cause).WithRetryable(apierror.False)
}

func NewEvalError(errStr string) apierror.Error {
	return apierror.New(apierror.KindInternal, "eval error: "+errStr).WithRetryable(apierror.False)
}
