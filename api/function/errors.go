package function

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

var (
	ErrRegistryNotFound = apierror.New(apierror.NotFound, "function registry not found in context").WithRetryable(apierror.False)

	ErrProcessContextNotFound = apierror.New(apierror.NotFound, "process context not found").WithRetryable(apierror.False)

	ErrCallNotFound = apierror.New(apierror.NotFound, "async call not found").WithRetryable(apierror.False)

	ErrNilContext = apierror.New(apierror.Invalid, "nil context").WithRetryable(apierror.False)

	ErrNilCallback = apierror.New(apierror.Invalid, "nil callback").WithRetryable(apierror.False)

	ErrNodeNotFound = apierror.New(apierror.NotFound, "relay node not configured").WithRetryable(apierror.False)

	ErrPIDNotFound = apierror.New(apierror.NotFound, "frame PID not found in context").WithRetryable(apierror.False)

	ErrPIDGeneratorNotFound = apierror.New(apierror.NotFound, "PID generator not found in context").WithRetryable(apierror.False)
)

func NewHandlerNotFoundError(id registry.ID) apierror.Error {
	return apierror.New(apierror.NotFound, "no handler registered for target: "+id.String()).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"target": id.String()}))
}

func NewInterceptorExistsError(name string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "interceptor \""+name+"\" already registered").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"name": name}))
}

func NewInterceptorNotFoundError(name string) apierror.Error {
	return apierror.New(apierror.NotFound, "interceptor \""+name+"\" not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"name": name}))
}

func NewInterceptorSealedError() apierror.Error {
	return apierror.New(apierror.Invalid, "interceptor registry is sealed").WithRetryable(apierror.False)
}
