package env

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewUnsupportedKindError(kind string) *Error {
	return &Error{
		kind:    apierror.KindInvalid,
		message: "unsupported entry kind: " + kind,
	}
}

func NewDecodeVariableError(cause error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to decode variable",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": cause.Error()}),
		cause:     cause,
	}
}
