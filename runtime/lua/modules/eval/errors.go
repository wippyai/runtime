package eval

import (
	apierror "github.com/wippyai/runtime/api/error"
)

func NewEvalError(errStr string) apierror.Error {
	return apierror.New(apierror.Internal, "eval error: "+errStr).WithRetryable(apierror.False)
}
