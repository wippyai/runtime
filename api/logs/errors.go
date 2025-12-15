package logs

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrGetConfigTimeout = apierror.New(apierror.KindTimeout, "timeout waiting for log config").WithRetryable(apierror.True)
	ErrSetConfigTimeout = apierror.New(apierror.KindTimeout, "timeout waiting for config confirmation").WithRetryable(apierror.True)
)

func NewContextCanceledError(err error) apierror.Error {
	return apierror.New(apierror.KindCanceled, "context canceled: "+err.Error()).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}
