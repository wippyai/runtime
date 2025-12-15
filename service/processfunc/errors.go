package processfunc

import apierror "github.com/wippyai/runtime/api/error"

var ErrMonitorChannelClosed = apierror.New(apierror.KindInternal, "monitor channel closed unexpectedly").WithRetryable(apierror.False)

func newRegisterPIDError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "register caller pid").WithCause(cause)
}

func newAttachRelayError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "attach to relay").WithCause(cause)
}

func newStartProcessError(cause error) apierror.Error {
	return apierror.New(apierror.KindInternal, "start process").WithCause(cause)
}
