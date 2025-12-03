package entry

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
	ErrConfigurationDataRequired = &Error{
		kind:      apierror.KindInvalid,
		message:   "configuration data is required",
		retryable: apierror.False,
	}

	ErrCannotReplaceEntireDataField = &Error{
		kind:      apierror.KindInvalid,
		message:   "cannot replace entire data field",
		retryable: apierror.False,
	}

	ErrCannotReplaceEntireMetaField = &Error{
		kind:      apierror.KindInvalid,
		message:   "cannot replace entire meta field",
		retryable: apierror.False,
	}

	ErrCannotAppendToEntireDataField = &Error{
		kind:      apierror.KindInvalid,
		message:   "cannot append to entire data field",
		retryable: apierror.False,
	}

	ErrCannotAppendToEntireMetaField = &Error{
		kind:      apierror.KindInvalid,
		message:   "cannot append to entire meta field",
		retryable: apierror.False,
	}

	ErrCannotDeleteEntireDataField = &Error{
		kind:      apierror.KindInvalid,
		message:   "cannot delete entire data field",
		retryable: apierror.False,
	}

	ErrCannotDeleteEntireMetaField = &Error{
		kind:      apierror.KindInvalid,
		message:   "cannot delete entire meta field",
		retryable: apierror.False,
	}

	ErrEmptyPath = &Error{
		kind:      apierror.KindInvalid,
		message:   "empty path",
		retryable: apierror.False,
	}

	ErrEmptyPathSegments = &Error{
		kind:      apierror.KindInvalid,
		message:   "empty path segments",
		retryable: apierror.False,
	}
)

func NewUnmarshalConfigError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to unmarshal config",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewInvalidConfigurationError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid configuration",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewInvalidTargetError(target string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "invalid target (must be 'data' or 'meta')",
		retryable: apierror.False,
		details: attrs.Bag{
			"target": target,
		},
	}
}

func NewTranscodeToGolangError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to transcode to golang format",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewCannotAppendToNonArrayError(path string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "cannot append to non-array field",
		retryable: apierror.False,
		details: attrs.Bag{
			"path": path,
		},
	}
}
