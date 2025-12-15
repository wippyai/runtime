package supervisor

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

const (
	KindTerminated apierror.Kind = "Terminated"
	KindExited     apierror.Kind = "Exited"
)

var (
	ErrTerminated         = apierror.New(KindTerminated, "service terminated").WithRetryable(apierror.False)
	ErrExit               = apierror.New(KindExited, "service exited").WithRetryable(apierror.False)
	ErrStartTimeout       = apierror.New(apierror.KindTimeout, "service start timed out").WithRetryable(apierror.True)
	ErrOutsideTransaction = apierror.New(apierror.KindInvalid, "action received outside of transaction").WithRetryable(apierror.False)
)

func NewInvalidDurationError(field string, cause error) apierror.Error {
	return apierror.E(
		apierror.KindInvalid,
		"invalid "+field+" duration format",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"field": field}),
		cause,
	)
}

func NewServiceNotFoundError(serviceID string) apierror.Error {
	return apierror.E(
		apierror.KindNotFound,
		"service "+serviceID+" not found",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"service_id": serviceID}),
		nil,
	)
}
