package msgpack

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
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

func NewInvalidFormatError(direction string, expected, got payload.Format) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   direction + " can only transcode from " + string(expected) + " format, got " + string(got),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"direction": direction, "expected": string(expected), "got": string(got)}),
	}
}

func NewInvalidDataTypeError(direction string, dataType string) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   direction + " can only handle []byte, got " + dataType,
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"direction": direction, "data_type": dataType}),
	}
}

func NewMarshalError(err error) *Error {
	return &Error{
		kind:      apierror.KindInternal,
		message:   "failed to marshal to MsgPack: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}

func NewUnmarshalError(err error) *Error {
	return &Error{
		kind:      apierror.KindInvalid,
		message:   "failed to unmarshal MsgPack: " + err.Error(),
		retryable: apierror.False,
		details:   attrs.NewBagFrom(map[string]any{"cause": err.Error()}),
		cause:     err,
	}
}
