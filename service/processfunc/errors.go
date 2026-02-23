// SPDX-License-Identifier: MPL-2.0

package processfunc

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var ErrMonitorChannelClosed = apierror.New(apierror.Internal, "monitor channel closed unexpectedly").WithRetryable(apierror.False)

func newRegisterPIDError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to register caller pid").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func newAttachRelayError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to attach to relay").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func newStartProcessError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to start process").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}
