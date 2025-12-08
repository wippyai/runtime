package payload

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors for payload operations.
var (
	ErrEmptyFormat = &Error{
		kind:      apierror.KindInvalid,
		message:   "payload format is empty",
		retryable: apierror.False,
	}
)

// Error implements apierror.Error for payload errors.
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

// NewNoTranscodingPathError creates an error when no transcoding path is found.
func NewNoTranscodingPathError(from, to Format) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "no transcoding path found from " + string(from) + " to " + string(to),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"from": string(from), "to": string(to)}),
	}
}

// NewNoTranscoderError creates an error when no transcoder is registered.
func NewNoTranscoderError(from, to string) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "no transcoder registered for " + from + " to " + to,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"from": from, "to": to}),
	}
}

// NewTranscodeError creates an error when transcoding fails.
func NewTranscodeError(from, to string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "error transcoding from " + from + " to " + to + ": " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"from": from, "to": to, "cause": err.Error()}),
		cause:     err,
	}
}

// NewNoUnmarshalPathError creates an error when no unmarshal path is found.
func NewNoUnmarshalPathError(format Format) *Error {
	return &Error{
		kind:      apierror.KindNotFound,
		message:   "no unmarshaling path found for format " + string(format),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"format": string(format)}),
	}
}

// NewUnmarshalTranscodeError creates an error when transcoding for unmarshal fails.
func NewUnmarshalTranscodeError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "error transcoding payload for unmarshaling: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

// NewUnmarshalerNotFoundError creates an error when unmarshaler is not found after path resolution.
func NewUnmarshalerNotFoundError(format string) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "unmarshaler not found for format " + format + ", even though a path was found",
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"format": format}),
	}
}

// NewInvalidFormatError creates an error for invalid format input during transcoding.
func NewInvalidFormatError(direction string, expected, got Format) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   direction + " can only transcode from " + string(expected) + " format, got " + string(got),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"direction": direction, "expected": string(expected), "got": string(got)}),
	}
}

// NewInvalidDataTypeError creates an error for unsupported data types.
func NewInvalidDataTypeError(direction string, expected string, dataType string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   direction + " can only handle " + expected + ", got " + dataType,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"direction": direction, "expected": expected, "data_type": dataType}),
	}
}

// NewMarshalError creates an error when marshaling fails.
func NewMarshalError(format string, err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to marshal to " + format + ": " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"format": format, "cause": err.Error()}),
		cause:     err,
	}
}

// NewUnmarshalError creates an error when unmarshaling fails.
func NewUnmarshalError(format string, err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to unmarshal " + format + ": " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"format": format, "cause": err.Error()}),
		cause:     err,
	}
}
