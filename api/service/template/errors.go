package template

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
	ErrEmptySource = &Error{
		kind:      apierror.KindInvalid,
		message:   "template source cannot be empty",
		retryable: apierror.False,
	}

	ErrEmptySetName = &Error{
		kind:      apierror.KindInvalid,
		message:   "template set name cannot be empty",
		retryable: apierror.False,
	}

	ErrEmptyDelimiters = &Error{
		kind:      apierror.KindInvalid,
		message:   "template delimiters cannot be empty",
		retryable: apierror.False,
	}

	ErrEmptyCommentDelimiters = &Error{
		kind:      apierror.KindInvalid,
		message:   "comment delimiters cannot be empty",
		retryable: apierror.False,
	}

	ErrConflictingDelimiters = &Error{
		kind:      apierror.KindInvalid,
		message:   "template and comment delimiters must be different",
		retryable: apierror.False,
	}

	ErrEmptyExtensions = &Error{
		kind:      apierror.KindInvalid,
		message:   "template extensions cannot be empty",
		retryable: apierror.False,
	}
)
