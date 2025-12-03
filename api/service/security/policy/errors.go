package policy

import (
	"fmt"

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
	ErrActionsStringEmpty = &Error{
		kind:      apierror.KindInvalid,
		message:   "actions string cannot be empty",
		retryable: apierror.False,
	}

	ErrActionsListEmpty = &Error{
		kind:      apierror.KindInvalid,
		message:   "actions list cannot be empty",
		retryable: apierror.False,
	}

	ErrActionsInvalidType = &Error{
		kind:      apierror.KindInvalid,
		message:   "actions must be either a string or a list of strings",
		retryable: apierror.False,
	}

	ErrResourcesStringEmpty = &Error{
		kind:      apierror.KindInvalid,
		message:   "resources string cannot be empty",
		retryable: apierror.False,
	}

	ErrResourcesListEmpty = &Error{
		kind:      apierror.KindInvalid,
		message:   "resources list cannot be empty",
		retryable: apierror.False,
	}

	ErrResourcesInvalidType = &Error{
		kind:      apierror.KindInvalid,
		message:   "resources must be either a string or a list of strings",
		retryable: apierror.False,
	}
)

func NewInvalidPolicyEffectError(effect Effect) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   fmt.Sprintf("invalid policy effect: %s", effect),
		retryable: apierror.False,
	}
}

func NewConditionFieldEmptyError(index int) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   fmt.Sprintf("condition[%d]: field cannot be empty", index),
		retryable: apierror.False,
	}
}

func NewConditionOperatorEmptyError(index int) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   fmt.Sprintf("condition[%d]: operator cannot be empty", index),
		retryable: apierror.False,
	}
}

func NewConditionValueRequiredError(index int) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   fmt.Sprintf("condition[%d]: either value or value_from must be provided", index),
		retryable: apierror.False,
	}
}

func NewConditionInvalidOperatorError(index int, operator string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   fmt.Sprintf("condition[%d]: invalid operator: %s", index, operator),
		retryable: apierror.False,
	}
}
