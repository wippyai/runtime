package evalhost

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

func NewCompileError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "compile failed",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewCreateProcessError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create process",
		retryable: apierror.False,
		cause:     cause,
	}
}

var ErrProcessFactoryNotAvailable = &Error{
	kind:      apierror.KindInternal,
	message:   "process factory not available",
	retryable: apierror.False,
}

func NewCreateProcessFromIDError(id string, cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to create process from ID " + id,
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewModuleNotAvailableError(name string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "module \"" + name + "\" is not available",
		retryable: apierror.False,
	}
}

func NewParseError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "parse error",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewCompileScriptError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "compile error",
		retryable: apierror.False,
		cause:     cause,
	}
}

func NewForbiddenClassError(module, class string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "module \"" + module + "\" has forbidden class \"" + class + "\"",
		retryable: apierror.False,
	}
}

func NewRunError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "run failed",
		retryable: apierror.False,
		cause:     cause,
	}
}
