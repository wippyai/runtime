package component

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Error implements apierror.Error for component errors
type Error struct {
	kind      apierror.Kind
	message   string
	retryable apierror.Ternary
	details   attrs.Attributes
}

func (e *Error) Error() string               { return e.message }
func (e *Error) Kind() apierror.Kind         { return e.kind }
func (e *Error) Retryable() apierror.Ternary { return e.retryable }
func (e *Error) Details() attrs.Attributes   { return e.details }

// Sentinel errors
var (
	ErrTranscoderNotFound = &Error{
		kind:      apierror.KindNotFound,
		message:   "transcoder not found in context",
		retryable: apierror.False,
	}
)

// NewUnmarshalError creates an error for config unmarshal failures
func NewUnmarshalError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to unmarshal config: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
	}
}

// NewValidationError creates an error for config validation failures
func NewValidationError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid configuration: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
	}
}
