package events

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error implements apierror.Error for event handler errors
type Error struct { // todo: mo away ?????
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

// NewConfigDataRequiredError creates an error when configuration data is missing
func NewConfigDataRequiredError() *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "configuration data is required for create/update operations",
		retryable: apierror.False,
	}
}

// NewUnknownEventKindError creates an error for unknown event kind
func NewUnknownEventKindError(kind string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unknown event kind: " + kind,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"event_kind": kind}),
	}
}
