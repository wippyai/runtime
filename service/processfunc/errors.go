package processfunc

import apierror "github.com/wippyai/runtime/api/error"

var ErrMonitorChannelClosed = apierror.New(apierror.Internal, "monitor channel closed unexpectedly").WithRetryable(apierror.False)

func newRegisterPIDError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "register caller pid").WithCause(cause)
}

func newAttachRelayError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "attach to relay").WithCause(cause)
}

func newStartProcessError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "start process").WithCause(cause)
}
