package bootconfig

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

func NewReadConfigFileError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to read config file",
		retryable: apierror.False,
		cause:     err,
	}
}

func NewParseYAMLError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to parse YAML",
		retryable: apierror.False,
		cause:     err,
	}
}

var (
	ErrMissingVersionField = &Error{
		kind:      apierror.KindInvalid,
		message:   "missing 'version' field in config file",
		retryable: apierror.False,
	}
)

func NewUnsupportedVersionError(version string, supported []string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "unsupported config version",
		retryable: apierror.False,
		details: attrs.Bag{
			"version":   version,
			"supported": supported,
		},
	}
}
