package pool

import (
	apierror "github.com/wippyai/runtime/api/error"
)

// Error implements apierror.Error for pool errors.
type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }

// ErrPoolClosed indicates the pool is closed.
var ErrPoolClosed = &Error{
	kind:      apierror.KindUnavailable,
	message:   "pool is closed",
	retryable: apierror.False,
}
