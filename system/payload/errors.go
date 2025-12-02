package payload

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
)

// Error implements apierror.Error for payload errors
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
	ErrEmptyFormat = &Error{
		kind:      apierror.KindInvalid,
		message:   "payload format is empty",
		retryable: apierror.False,
	}
)

// NewNoTranscodingPathError creates an error when no transcoding path is found
func NewNoTranscodingPathError(from, to payload.Format) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "no transcoding path found from " + string(from) + " to " + string(to),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"from": string(from), "to": string(to)}),
	}
}

// NewNoTranscoderError creates an error when no transcoder is registered
func NewNoTranscoderError(from, to string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "no transcoder registered for " + from + " to " + to,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"from": from, "to": to}),
	}
}

// NewTranscodeError creates an error when transcoding fails
func NewTranscodeError(from, to string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "error transcoding from " + from + " to " + to + ": " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"from": from, "to": to, "cause": err.Error()}),
		cause:     err,
	}
}

// NewNoUnmarshalPathError creates an error when no unmarshal path is found
func NewNoUnmarshalPathError(format payload.Format) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "no unmarshaling path found for format " + string(format),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"format": string(format)}),
	}
}

// NewUnmarshalTranscodeError creates an error when transcoding for unmarshal fails
func NewUnmarshalTranscodeError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "error transcoding payload for unmarshaling: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewUnmarshalerNotFoundError creates an error when unmarshaler is not found after path resolution
func NewUnmarshalerNotFoundError(format string) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "unmarshaler not found for format " + format + ", even though a path was found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"format": format}),
	}
}
