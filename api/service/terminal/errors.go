package terminal

import (
	"errors"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors for terminal host operations.
var (
	ErrHostNotRunning     = errors.New("host is not running")
	ErrHostShuttingDown   = errors.New("host is shutting down")
	ErrHostAlreadyRunning = errors.New("host already running")
)

// Error implements apierror.Error for terminal errors.
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

// NewDecodeConfigError creates a config decode error.
func NewDecodeConfigError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode terminal config",
		retryable: apierror.False,
		cause:     cause,
	}
}
