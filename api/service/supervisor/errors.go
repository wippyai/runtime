package supervisor

import (
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/pid"
)

var (
	ErrProcessRequired = apierror.New(apierror.Invalid, "process is required").WithRetryable(apierror.False)
	ErrHostRequired    = apierror.New(apierror.Invalid, "host is required").WithRetryable(apierror.False)
)

func NewInvalidHostError(hostID pid.HostID) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid host: "+string(hostID)).WithRetryable(apierror.False)
}
