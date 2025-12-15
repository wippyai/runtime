package payload

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
)

// NewNoTranscodingPathError creates an error when no transcoding path is found.
func NewNoTranscodingPathError(from, to payload.Format) apierror.Error {
	return apierror.E(
		apierror.NotFound,
		"no transcoding path found from "+from+" to "+to,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"from": from, "to": to}),
		nil,
	)
}

// NewNoTranscoderError creates an error when no transcoder is registered.
func NewNoTranscoderError(from, to string) apierror.Error {
	return apierror.E(
		apierror.NotFound,
		"no transcoder registered for "+from+" to "+to,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"from": from, "to": to}),
		nil,
	)
}

// NewNoUnmarshalPathError creates an error when no unmarshal path is found.
func NewNoUnmarshalPathError(format payload.Format) apierror.Error {
	return apierror.E(
		apierror.NotFound,
		"no unmarshaling path found for format "+format,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"format": format}),
		nil,
	)
}

// NewInvalidFormatError creates an error for invalid format input during transcoding.
func NewInvalidFormatError(direction string, expected, got payload.Format) apierror.Error {
	return apierror.E(
		apierror.Invalid,
		direction+" can only transcode from "+expected+" format, got "+got,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"direction": direction, "expected": expected, "got": got}),
		nil,
	)
}

// NewInvalidDataTypeError creates an error for unsupported data types.
func NewInvalidDataTypeError(direction string, expected string, dataType string) apierror.Error {
	return apierror.E(
		apierror.Invalid,
		direction+" can only handle "+expected+", got "+dataType,
		apierror.False,
		attrs.NewBagFrom(map[string]any{"direction": direction, "expected": expected, "data_type": dataType}),
		nil,
	)
}

// NewUnmarshalError creates an error when unmarshaling fails.
func NewUnmarshalError(format string, err error) apierror.Error {
	return apierror.E(
		apierror.Invalid,
		"failed to unmarshal "+format+": "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"format": format, "cause": err.Error()}),
		err,
	)
}

// NewTranscodeError creates an error when transcoding fails.
func NewTranscodeError(from, to string, err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"error transcoding from "+from+" to "+to+": "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"from": from, "to": to, "cause": err.Error()}),
		err,
	)
}

// NewUnmarshalTranscodeError creates an error when transcoding for unmarshal fails.
func NewUnmarshalTranscodeError(err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"error transcoding payload for unmarshaling: "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		err,
	)
}

// NewUnmarshalerNotFoundError creates an error when unmarshaler is not found after path resolution.
func NewUnmarshalerNotFoundError(format string) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"unmarshaler not found for format "+format+", even though a path was found",
		apierror.False,
		attrs.NewBagFrom(map[string]any{"format": format}),
		nil,
	)
}

// NewMarshalError creates an error when marshaling fails.
func NewMarshalError(format string, err error) apierror.Error {
	return apierror.E(
		apierror.Internal,
		"failed to marshal to "+format+": "+err.Error(),
		apierror.False,
		attrs.NewBagFrom(map[string]any{"format": format, "cause": err.Error()}),
		err,
	)
}
