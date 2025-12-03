package http

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
	cause     error
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }
func (e *Error) Unwrap() error               { return e.cause }

var (
	ErrTranscoderRequired              = &Error{kind: apierror.KindInvalid, message: "transcoder is required"}
	ErrEventBusRequired                = &Error{kind: apierror.KindInvalid, message: "event bus is required"}
	ErrServerFactoryRequired           = &Error{kind: apierror.KindInvalid, message: "server factory is required"}
	ErrEndpointFactoryRequired         = &Error{kind: apierror.KindInvalid, message: "endpoint factory is required"}
	ErrStaticFactoryRequired           = &Error{kind: apierror.KindInvalid, message: "static factory is required"}
	ErrConfigDataRequired              = &Error{kind: apierror.KindInvalid, message: "configuration data is required"}
	ErrFunctionRegistryRequired        = &Error{kind: apierror.KindInvalid, message: "function registry is required"}
	ErrFilesystemRegistryRequired      = &Error{kind: apierror.KindInvalid, message: "filesystem registry is required"}
	ErrMiddlewareFactoryRequired       = &Error{kind: apierror.KindInvalid, message: "middleware factory is required"}
	ErrIndexFileRequired               = &Error{kind: apierror.KindInvalid, message: "index file must be specified for SPA mode"}
	ErrPathCannotBeEmpty               = &Error{kind: apierror.KindInvalid, message: "path cannot be empty"}
	ErrMountPathCannotBeEmpty          = &Error{kind: apierror.KindInvalid, message: "mount path cannot be empty"}
	ErrServerAddressChangeWhileRunning = &Error{kind: apierror.KindConflict, message: "cannot change server address while running"}
	ErrServerHostNotInitialized        = &Error{kind: apierror.KindInternal, message: "server host not initialized"}
)

func NewUnsupportedEntryKindError(kind string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "unsupported entry kind: " + kind,
	}
}

func NewServerAlreadyExistsError(id string) *Error {
	return &Error{
		kind:    apierror.KindAlreadyExists,
		message: "server " + id + " already exists",
	}
}

func NewServerNotFoundError(id string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: "server " + id + " not found",
	}
}

func NewRouterNotFoundError(id string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: "router " + id + " not found",
	}
}

func NewEndpointNotFoundError(id string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: "endpoint " + id + " not found",
	}
}

func NewStaticHandlerNotFoundError(id string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: "static handler " + id + " not found",
	}
}

func NewFilesystemNotFoundError(id string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: "filesystem not found: " + id,
	}
}

func NewMountNotFoundError(id string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: "mount for Source " + id + " not found",
	}
}

func NewMountPathNotFoundError(path string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: "mount path " + path + " not found",
	}
}

func NewRouteNotFoundError(id, routerID string) *Error {
	return &Error{
		kind:    apierror.KindNotFound,
		message: "route " + id + " not found in router " + routerID,
	}
}

func NewRouterPrefixExistsError(prefix string) *Error {
	return &Error{
		kind:    apierror.KindAlreadyExists,
		message: "router with prefix " + prefix + " already exists",
	}
}

func NewMountPathExistsError(path string) *Error {
	return &Error{
		kind:    apierror.KindAlreadyExists,
		message: "mount path " + path + " already exists",
	}
}

func NewInvalidHTTPMethodError(method string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "invalid HTTP method: " + method,
	}
}

func NewInvalidPathError(path string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "path must start with /: " + path,
	}
}

func NewInvalidMountPathError(path string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "mount path must start with /: " + path,
	}
}

func NewUpdateConfigError(err error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to update server config",
		cause:   err,
	}
}

func NewUnmarshalConfigError(err error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "failed to unmarshal config",
		cause:   err,
	}
}

func NewInvalidConfigError(err error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "invalid configuration",
		cause:   err,
	}
}

func NewInvalidEndpointConfigError(err error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "invalid endpoint config",
		cause:   err,
	}
}

func NewInvalidStaticConfigError(err error) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "invalid static config",
		cause:   err,
	}
}

func NewMiddlewareCreateError(name string, err error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to create middleware " + name,
		cause:   err,
	}
}

func NewPostMiddlewareCreateError(name string, err error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "failed to create post-match middleware " + name,
		cause:   err,
	}
}

func NewServerError(err error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "server error",
		cause:   err,
	}
}

func NewStartupCheckError(err error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "startup check failed",
		cause:   err,
	}
}

func NewShutdownError(err error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "shutdown error",
		cause:   err,
	}
}

func NewGracefulShutdownError(err error) *Error {
	return &Error{
		kind:    apierror.KindInternal,
		message: "graceful shutdown failed",
		cause:   err,
	}
}

func NewStartupTimeoutError(timeout string) *Error {
	return &Error{
		kind:    apierror.KindTimeout,
		message: "service failed to start within " + timeout + " timeout",
	}
}

func NewStartupCanceledError(err error) *Error {
	return &Error{
		kind:    apierror.KindCanceled,
		message: "startup canceled",
		cause:   err,
	}
}
