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
	ErrServerIDRequired                = apierror.New(apierror.Invalid, "server ID is required").WithRetryable(apierror.False)
	ErrAddressRequired                 = apierror.New(apierror.Invalid, "address is required").WithRetryable(apierror.False)
	ErrInvalidWorkers                  = apierror.New(apierror.Invalid, "workers must be greater than 0").WithRetryable(apierror.False)
	ErrInvalidReadTimeout              = apierror.New(apierror.Invalid, "read_timeout must be greater than 0").WithRetryable(apierror.False)
	ErrInvalidWriteTimeout             = apierror.New(apierror.Invalid, "write_timeout must be greater than 0").WithRetryable(apierror.False)
	ErrEmptyAddr                       = apierror.New(apierror.Invalid, "address cannot be empty").WithRetryable(apierror.False)
	ErrNilMetadata                     = apierror.New(apierror.Invalid, "metadata cannot be nil").WithRetryable(apierror.False)
	ErrEmptyFuncName                   = apierror.New(apierror.Invalid, "function name cannot be empty").WithRetryable(apierror.False)
	ErrEmptyPath                       = apierror.New(apierror.Invalid, "path cannot be empty").WithRetryable(apierror.False)
	ErrEmptyMethod                     = apierror.New(apierror.Invalid, "method cannot be empty").WithRetryable(apierror.False)
)

func NewUnsupportedEntryKindError(kind string) apierror.Error {
	return apierror.New(apierror.Invalid, "unsupported entry kind: "+kind).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewServerAlreadyExistsError(id string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "server "+id+" already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewServerNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "server "+id+" not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewInvalidReadTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid read_timeout duration format").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidWriteTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid write_timeout duration format").WithCause(cause).WithRetryable(apierror.False)
}

func NewDecodeConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to decode config").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidDurationError(field string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid "+field+" duration format").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidTimeoutConfigError(cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid timeout config").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidTimeoutError(field string) apierror.Error {
	return apierror.New(apierror.Invalid, field+" must be greater than 0").WithRetryable(apierror.False)
}

func NewNegativeConfigError(field string) apierror.Error {
	return apierror.New(apierror.Invalid, field+" cannot be negative").WithRetryable(apierror.False)
}

func NewMissingMetadataError(field string) apierror.Error {
	return apierror.New(apierror.Invalid, "missing metadata field: "+field).WithRetryable(apierror.False)
}

func NewPathMustStartWithSlashError() apierror.Error {
	return apierror.New(apierror.Invalid, "path must start with /").WithRetryable(apierror.False)
}

func NewInvalidHTTPMethodError(method string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid HTTP method: "+method).WithRetryable(apierror.False)
}

func NewRouterNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "router "+id+" not found")
}

func NewEndpointNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "endpoint "+id+" not found")
}

func NewStaticHandlerNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "static handler "+id+" not found")
}

func NewFilesystemNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "filesystem not found: "+id)
}

func NewMountNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "mount for Source "+id+" not found")
}

func NewMountPathNotFoundError(path string) apierror.Error {
	return apierror.New(apierror.NotFound, "mount path "+path+" not found")
}

func NewRouteNotFoundError(id, routerID string) apierror.Error {
	return apierror.New(apierror.NotFound, "route "+id+" not found in router "+routerID)
}

func NewRouterPrefixExistsError(prefix string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "router with prefix "+prefix+" already exists")
}

func NewMountPathExistsError(path string) apierror.Error {
	return apierror.New(apierror.AlreadyExists, "mount path "+path+" already exists")
}

func NewInvalidPathError(path string) apierror.Error {
	return apierror.New(apierror.Invalid, "path must start with /: "+path)
}

func NewInvalidMountPathError(path string) apierror.Error {
	return apierror.New(apierror.Invalid, "mount path must start with /: "+path)
}

func NewUpdateConfigError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to update server config").WithCause(err)
}

func NewUnmarshalConfigError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to unmarshal config").WithCause(err)
}

func NewInvalidConfigError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid configuration").WithCause(err)
}

func NewInvalidEndpointConfigError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid endpoint config").WithCause(err)
}

func NewInvalidStaticConfigError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid static config").WithCause(err)
}

func NewMiddlewareCreateError(name string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create middleware "+name).WithCause(err)
}

func NewPostMiddlewareCreateError(name string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create post-match middleware "+name).WithCause(err)
}

func NewServerError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "http server error: "+err.Error()).WithCause(err)
}

func NewStartupCheckError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "startup check failed").WithCause(err)
}

func NewShutdownError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "shutdown error").WithCause(err)
}

func NewGracefulShutdownError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "graceful shutdown failed").WithCause(err)
}

func NewStartupTimeoutError(timeout string) apierror.Error {
	return apierror.New(apierror.Timeout, "service failed to start within "+timeout+" timeout")
}

func NewStartupCanceledError(err error) apierror.Error {
	return apierror.New(apierror.Canceled, "startup canceled").WithCause(err)
}
