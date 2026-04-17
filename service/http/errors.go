// SPDX-License-Identifier: MPL-2.0

package http

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrTranscoderRequired              = apierror.New(apierror.Invalid, "transcoder is required").WithRetryable(apierror.False)
	ErrEventBusRequired                = apierror.New(apierror.Invalid, "event bus is required").WithRetryable(apierror.False)
	ErrServerFactoryRequired           = apierror.New(apierror.Invalid, "server factory is required").WithRetryable(apierror.False)
	ErrEndpointFactoryRequired         = apierror.New(apierror.Invalid, "endpoint factory is required").WithRetryable(apierror.False)
	ErrStaticFactoryRequired           = apierror.New(apierror.Invalid, "static factory is required").WithRetryable(apierror.False)
	ErrConfigDataRequired              = apierror.New(apierror.Invalid, "configuration data is required").WithRetryable(apierror.False)
	ErrFunctionRegistryRequired        = apierror.New(apierror.Invalid, "function registry is required").WithRetryable(apierror.False)
	ErrFilesystemRegistryRequired      = apierror.New(apierror.Invalid, "filesystem registry is required").WithRetryable(apierror.False)
	ErrMiddlewareFactoryRequired       = apierror.New(apierror.Invalid, "middleware factory is required").WithRetryable(apierror.False)
	ErrIndexFileRequired               = apierror.New(apierror.Invalid, "index file must be specified for SPA mode").WithRetryable(apierror.False)
	ErrPathCannotBeEmpty               = apierror.New(apierror.Invalid, "path cannot be empty").WithRetryable(apierror.False)
	ErrMountPathCannotBeEmpty          = apierror.New(apierror.Invalid, "mount path cannot be empty").WithRetryable(apierror.False)
	ErrServerAddressChangeWhileRunning = apierror.New(apierror.Conflict, "cannot change server address while running").WithRetryable(apierror.False)
	ErrServerHostNotInitialized        = apierror.New(apierror.Internal, "server host not initialized").WithRetryable(apierror.False)
	ErrNetworkRegistryNotAvailable     = apierror.New(apierror.Internal, "network registry not available in context").WithRetryable(apierror.False)
	ErrClearnetAutoTLSUnsupported      = apierror.New(apierror.Invalid, "tls.mode=auto requires an overlay network driver").WithRetryable(apierror.False)
	ErrTLSEnvRegistryUnavailable       = apierror.New(apierror.Internal, "env registry not available in context").WithRetryable(apierror.False)
)

func NewUnsupportedEntryKindError(kind string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported entry kind").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewServerAlreadyExistsError(id string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "server already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewServerNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "server not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewRouterNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "router not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"router_id": id}))
}

func NewEndpointNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "endpoint not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"endpoint_id": id}))
}

func NewStaticHandlerNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "static handler not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"handler_id": id}))
}

func NewFilesystemNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "filesystem not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"filesystem_id": id}))
}

func NewMountNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "mount not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"source_id": id}))
}

func NewMountPathNotFoundError(path string) apierror.Error {
	return apierror.New(apierror.NotFound, "mount path not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path}))
}

func NewRouteNotFoundError(id, routerID string) apierror.Error {
	return apierror.New(apierror.NotFound, "route not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"route_id": id, "router_id": routerID}))
}

func NewRouterPrefixExistsError(prefix string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "router prefix already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"prefix": prefix}))
}

func NewMountPathExistsError(path string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "mount path already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path}))
}

func NewInvalidPathError(path string) apierror.Error {
	return apierror.New(apierror.Invalid, "path must start with /").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path}))
}

func NewInvalidMountPathError(path string) apierror.Error {
	return apierror.New(apierror.Invalid, "mount path must start with /").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path}))
}

func NewUpdateConfigError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to update server config").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewUnmarshalConfigError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to unmarshal config").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewInvalidConfigError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid configuration").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewInvalidEndpointConfigError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid endpoint config").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewInvalidStaticConfigError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid static config").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewMiddlewareCreateError(name string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create middleware").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"middleware": name, "cause": err.Error()})).
		WithCause(err)
}

func NewPostMiddlewareCreateError(name string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create post-match middleware").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"middleware": name, "cause": err.Error()})).
		WithCause(err)
}

func NewServerError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "http server error").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewStartupCheckError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "startup check failed").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewShutdownError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "shutdown error").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewGracefulShutdownError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "graceful shutdown failed").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewStartupTimeoutError(timeout string) apierror.Error {
	return apierror.New(apierror.Timeout, "service failed to start within "+timeout+" timeout").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"timeout": timeout}))
}

func NewStartupCanceledError(err error) apierror.Error {
	return apierror.New(apierror.Canceled, "startup canceled").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewNetworkResolveError(id string, err error) apierror.Error {
	return apierror.New(apierror.NotFound, "overlay network not found: "+id).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"network": id, "cause": err.Error()})).
		WithCause(err)
}

func NewNetworkBindDeniedError(id string) apierror.Error {
	return apierror.New(apierror.PermissionDenied, "not allowed: bind on network "+id).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"network": id}))
}

func NewNetworkListenError(id string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "overlay listen failed: "+id).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"network": id, "cause": err.Error()})).
		WithCause(err)
}

func NewNetworkAutoTLSUnsupportedError(id string) apierror.Error {
	return apierror.New(apierror.Invalid, "network driver does not support auto TLS: "+id).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"network": id}))
}

func NewTLSLoadError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to load TLS cert/key").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

func NewTLSEnvResolveError(name string, err error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to resolve TLS env variable: "+name).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"variable": name, "cause": err.Error()})).
		WithCause(err)
}

func NewTLSCAParseError() apierror.Error {
	return apierror.New(apierror.Invalid, "client_ca PEM contains no valid certificates").WithRetryable(apierror.False)
}

func NewRouteConflictsError(conflicts []string) apierror.Error {
	msg := "route pattern conflicts detected:\n"
	for _, c := range conflicts {
		msg += "  - " + c + "\n"
	}
	return apierror.New(apierror.Conflict, msg).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"conflicts": conflicts,
		}))
}
