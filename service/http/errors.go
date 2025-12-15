package http

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrTranscoderRequired              = apierror.New(apierror.KindInvalid, "transcoder is required").WithRetryable(apierror.False)
	ErrEventBusRequired                = apierror.New(apierror.KindInvalid, "event bus is required").WithRetryable(apierror.False)
	ErrServerFactoryRequired           = apierror.New(apierror.KindInvalid, "server factory is required").WithRetryable(apierror.False)
	ErrEndpointFactoryRequired         = apierror.New(apierror.KindInvalid, "endpoint factory is required").WithRetryable(apierror.False)
	ErrStaticFactoryRequired           = apierror.New(apierror.KindInvalid, "static factory is required").WithRetryable(apierror.False)
	ErrConfigDataRequired              = apierror.New(apierror.KindInvalid, "configuration data is required").WithRetryable(apierror.False)
	ErrFunctionRegistryRequired        = apierror.New(apierror.KindInvalid, "function registry is required").WithRetryable(apierror.False)
	ErrFilesystemRegistryRequired      = apierror.New(apierror.KindInvalid, "filesystem registry is required").WithRetryable(apierror.False)
	ErrMiddlewareFactoryRequired       = apierror.New(apierror.KindInvalid, "middleware factory is required").WithRetryable(apierror.False)
	ErrIndexFileRequired               = apierror.New(apierror.KindInvalid, "index file must be specified for SPA mode").WithRetryable(apierror.False)
	ErrPathCannotBeEmpty               = apierror.New(apierror.KindInvalid, "path cannot be empty").WithRetryable(apierror.False)
	ErrMountPathCannotBeEmpty          = apierror.New(apierror.KindInvalid, "mount path cannot be empty").WithRetryable(apierror.False)
	ErrServerAddressChangeWhileRunning = apierror.New(apierror.KindConflict, "cannot change server address while running").WithRetryable(apierror.False)
	ErrServerHostNotInitialized        = apierror.New(apierror.KindInternal, "server host not initialized").WithRetryable(apierror.False)
	ErrServerIDRequired                = apierror.New(apierror.KindInvalid, "server ID is required").WithRetryable(apierror.False)
	ErrAddressRequired                 = apierror.New(apierror.KindInvalid, "address is required").WithRetryable(apierror.False)
	ErrInvalidWorkers                  = apierror.New(apierror.KindInvalid, "workers must be greater than 0").WithRetryable(apierror.False)
	ErrInvalidReadTimeout              = apierror.New(apierror.KindInvalid, "read_timeout must be greater than 0").WithRetryable(apierror.False)
	ErrInvalidWriteTimeout             = apierror.New(apierror.KindInvalid, "write_timeout must be greater than 0").WithRetryable(apierror.False)
	ErrEmptyAddr                       = apierror.New(apierror.KindInvalid, "address cannot be empty").WithRetryable(apierror.False)
	ErrNilMetadata                     = apierror.New(apierror.KindInvalid, "metadata cannot be nil").WithRetryable(apierror.False)
	ErrEmptyFuncName                   = apierror.New(apierror.KindInvalid, "function name cannot be empty").WithRetryable(apierror.False)
	ErrEmptyPath                       = apierror.New(apierror.KindInvalid, "path cannot be empty").WithRetryable(apierror.False)
	ErrEmptyMethod                     = apierror.New(apierror.KindInvalid, "method cannot be empty").WithRetryable(apierror.False)
)

func NewUnsupportedEntryKindError(kind string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "unsupported entry kind: "+kind).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"kind": kind}))
}

func NewServerAlreadyExistsError(id string) apierror.Error {
	return apierror.New(apierror.KindAlreadyExists, "server "+id+" already exists").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewServerNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "server "+id+" not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"id": id}))
}

func NewInvalidReadTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid read_timeout duration format").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidWriteTimeoutError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid write_timeout duration format").WithCause(cause).WithRetryable(apierror.False)
}

func NewDecodeConfigError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to decode config").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidDurationError(field string, cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid "+field+" duration format").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidTimeoutConfigError(cause error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid timeout config").WithCause(cause).WithRetryable(apierror.False)
}

func NewInvalidTimeoutError(field string) apierror.Error {
	return apierror.New(apierror.KindInvalid, field+" must be greater than 0").WithRetryable(apierror.False)
}

func NewNegativeConfigError(field string) apierror.Error {
	return apierror.New(apierror.KindInvalid, field+" cannot be negative").WithRetryable(apierror.False)
}

func NewMissingMetadataError(field string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "missing metadata field: "+field).WithRetryable(apierror.False)
}

func NewPathMustStartWithSlashError() apierror.Error {
	return apierror.New(apierror.KindInvalid, "path must start with /").WithRetryable(apierror.False)
}

func NewInvalidHTTPMethodError(method string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid HTTP method: "+method).WithRetryable(apierror.False)
}

func NewRouterNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "router "+id+" not found")
}

func NewEndpointNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "endpoint "+id+" not found")
}

func NewStaticHandlerNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "static handler "+id+" not found")
}

func NewFilesystemNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "filesystem not found: "+id)
}

func NewMountNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "mount for Source "+id+" not found")
}

func NewMountPathNotFoundError(path string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "mount path "+path+" not found")
}

func NewRouteNotFoundError(id, routerID string) apierror.Error {
	return apierror.New(apierror.KindNotFound, "route "+id+" not found in router "+routerID)
}

func NewRouterPrefixExistsError(prefix string) apierror.Error {
	return apierror.New(apierror.KindAlreadyExists, "router with prefix "+prefix+" already exists")
}

func NewMountPathExistsError(path string) apierror.Error {
	return apierror.New(apierror.KindAlreadyExists, "mount path "+path+" already exists")
}

func NewInvalidPathError(path string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "path must start with /: "+path)
}

func NewInvalidMountPathError(path string) apierror.Error {
	return apierror.New(apierror.KindInvalid, "mount path must start with /: "+path)
}

func NewUpdateConfigError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to update server config").WithCause(err)
}

func NewUnmarshalConfigError(err error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "failed to unmarshal config").WithCause(err)
}

func NewInvalidConfigError(err error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid configuration").WithCause(err)
}

func NewInvalidEndpointConfigError(err error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid endpoint config").WithCause(err)
}

func NewInvalidStaticConfigError(err error) apierror.Error {
	return apierror.New(apierror.KindInvalid, "invalid static config").WithCause(err)
}

func NewMiddlewareCreateError(name string, err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create middleware "+name).WithCause(err)
}

func NewPostMiddlewareCreateError(name string, err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "failed to create post-match middleware "+name).WithCause(err)
}

func NewServerError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "http server error: "+err.Error()).WithCause(err)
}

func NewStartupCheckError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "startup check failed").WithCause(err)
}

func NewShutdownError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "shutdown error").WithCause(err)
}

func NewGracefulShutdownError(err error) apierror.Error {
	return apierror.New(apierror.KindInternal, "graceful shutdown failed").WithCause(err)
}

func NewStartupTimeoutError(timeout string) apierror.Error {
	return apierror.New(apierror.KindTimeout, "service failed to start within "+timeout+" timeout")
}

func NewStartupCanceledError(err error) apierror.Error {
	return apierror.New(apierror.KindCanceled, "startup canceled").WithCause(err)
}
