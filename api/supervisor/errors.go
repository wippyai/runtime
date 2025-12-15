package supervisor

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

const (
	Terminated apierror.Kind = "Terminated"
	Exited     apierror.Kind = "Exited"
)

var (
	ErrTerminated         = apierror.New(Terminated, "service terminated").WithRetryable(apierror.False)
	ErrExit               = apierror.New(Exited, "service exited").WithRetryable(apierror.False)
	ErrOutsideTransaction = apierror.New(apierror.Invalid, "action received outside of transaction").WithRetryable(apierror.False)
)

func NewInvalidDurationError(field string, cause error) apierror.Error {
	return apierror.E(
		apierror.Invalid,
		"invalid "+field+" duration format",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"field": field}),
		cause,
	)
}
