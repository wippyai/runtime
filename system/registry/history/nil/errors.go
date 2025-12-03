package nil

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error implements apierror.Error for history errors
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

// Sentinel errors
var (
	ErrNoHeadVersion        = &Error{kind: apierror.KindNotFound, message: "no head version set", retryable: apierror.False}
	ErrHistoryNotAvailable  = &Error{kind: apierror.KindUnavailable, message: "version history not available: registry configured with history disabled (enable_history=false)", retryable: apierror.False}
	ErrRollbackNotSupported = &Error{kind: apierror.KindUnavailable, message: "version rollback not supported: registry configured with history disabled (enable_history=false)", retryable: apierror.False}
)
