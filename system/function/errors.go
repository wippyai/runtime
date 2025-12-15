package function

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

var (
	ErrCallCancelled = apierror.New(apierror.KindCanceled, "async call cancelled").WithRetryable(apierror.False)
)

func NewInvalidHandlerError(id registry.ID) apierror.Error {
	return apierror.New(apierror.KindInternal, "invalid handler type for target: "+id.String()).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"target": id.String()}))
}

func NewFrameContextError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to set frame context: "+err.Error()).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewSubscriberError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create subscriber: "+err.Error()).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}
