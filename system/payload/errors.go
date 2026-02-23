// SPDX-License-Identifier: MPL-2.0

package payload

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// NewNoTranscodingPathError creates an error when no transcoding path is found.
func NewNoTranscodingPathError(from, to string) apierror.Error {
	return apierror.New(apierror.NotFound, "no transcoding path found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"from": from, "to": to}))
}

// NewNoTranscoderError creates an error when no transcoder is registered.
func NewNoTranscoderError(from, to string) apierror.Error {
	return apierror.New(apierror.NotFound, "no transcoder registered").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"from": from, "to": to}))
}

// NewNoUnmarshalPathError creates an error when no unmarshal path is found.
func NewNoUnmarshalPathError(format string) apierror.Error {
	return apierror.New(apierror.NotFound, "no unmarshaling path found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"format": format}))
}

// NewInvalidFormatError creates an error for invalid format input during transcoding.
func NewInvalidFormatError(direction string, expected, got string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid format for "+direction).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"direction": direction, "expected": expected, "got": got}))
}

// NewInvalidDataTypeError creates an error for unsupported data types.
func NewInvalidDataTypeError(direction string, expected string, dataType string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid data type for "+direction).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"direction": direction, "expected": expected, "data_type": dataType}))
}

// NewUnmarshalError creates an error when unmarshaling fails.
func NewUnmarshalError(format string, err error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to unmarshal payload").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"format": format, "cause": err.Error()})).
		WithCause(err)
}

// NewTranscodeError creates an error when transcoding fails.
func NewTranscodeError(from, to string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "error transcoding payload").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"from": from, "to": to, "cause": err.Error()})).
		WithCause(err)
}

// NewUnmarshalTranscodeError creates an error when transcoding for unmarshal fails.
func NewUnmarshalTranscodeError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "error transcoding payload for unmarshaling").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": err.Error()})).
		WithCause(err)
}

// NewUnmarshalerNotFoundError creates an error when unmarshaler is not found after path resolution.
func NewUnmarshalerNotFoundError(format string) apierror.Error {
	return apierror.New(apierror.Internal, "unmarshaler not found").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"format": format}))
}

// NewMarshalError creates an error when marshaling fails.
func NewMarshalError(format string, err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to marshal payload").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"format": format, "cause": err.Error()})).
		WithCause(err)
}
