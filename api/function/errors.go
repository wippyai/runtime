package function

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

var (
	ErrRegistryNotFound = apierror.New(apierror.KindNotFound, "function registry not found in context").WithRetryable(apierror.False)

	ErrProcessContextNotFound = apierror.New(apierror.KindNotFound, "process context not found").WithRetryable(apierror.False)

	ErrCallNotFound = apierror.New(apierror.KindNotFound, "async call not found").WithRetryable(apierror.False)

	ErrCallCancelled = apierror.New(apierror.KindCanceled, "async call cancelled").WithRetryable(apierror.False)

	ErrNilContext = apierror.New(apierror.KindInvalid, "nil context").WithRetryable(apierror.False)

	ErrNilCallback = apierror.New(apierror.KindInvalid, "nil callback").WithRetryable(apierror.False)

	ErrNodeNotFound = apierror.New(apierror.KindNotFound, "relay node not configured").WithRetryable(apierror.False)

	ErrPIDNotFound = apierror.New(apierror.KindNotFound, "frame PID not found in context").WithRetryable(apierror.False)

	ErrPIDGeneratorNotFound = apierror.New(apierror.KindNotFound, "PID generator not found in context").WithRetryable(apierror.False)
)

func NewHandlerNotFoundError(id registry.ID) apierror.Error {
	return apierror.New(apierror.KindNotFound, "no handler registered for target: "+id.String()).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"target": id.String()}))
}

func NewInterceptorExistsError(name string) apierror.Error {
	return apierror.New(apierror.KindAlreadyExists, "interceptor \""+name+"\" already registered").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"name": name}))
}

func NewInterceptorNotFoundError(name string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "interceptor \""+name+"\" not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"name": name}))
}

func NewInterceptorSealedError() apierror.Error {
	return apierror.New(apierror.KindInvalid, "interceptor registry is sealed").WithRetryable(apierror.False)
}
