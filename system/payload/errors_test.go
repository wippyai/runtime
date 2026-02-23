// SPDX-License-Identifier: MPL-2.0

package payload_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/system/payload"
)

func TestErrorConstructors(t *testing.T) {
	t.Run("NewTranscodeError", func(t *testing.T) {
		cause := errors.New("test error")
		err := payload.NewTranscodeError("json", "golang", cause)
		assert.Contains(t, err.Error(), "error transcoding payload")
		assert.Equal(t, apierror.Internal, err.Kind())
		assert.True(t, errors.Is(err, cause))
		from, _ := err.Details().Get("from")
		assert.Equal(t, "json", from)
		to, _ := err.Details().Get("to")
		assert.Equal(t, "golang", to)
		detailCause, _ := err.Details().Get("cause")
		assert.Equal(t, "test error", detailCause)
	})

	t.Run("NewUnmarshalTranscodeError", func(t *testing.T) {
		cause := errors.New("test error")
		err := payload.NewUnmarshalTranscodeError(cause)
		assert.Contains(t, err.Error(), "error transcoding payload for unmarshaling")
		assert.Equal(t, apierror.Internal, err.Kind())
		assert.True(t, errors.Is(err, cause))
		detailCause, _ := err.Details().Get("cause")
		assert.Equal(t, "test error", detailCause)
	})

	t.Run("NewUnmarshalerNotFoundError", func(t *testing.T) {
		err := payload.NewUnmarshalerNotFoundError("json")
		assert.Contains(t, err.Error(), "unmarshaler not found")
		assert.Equal(t, apierror.Internal, err.Kind())
		format, _ := err.Details().Get("format")
		assert.Equal(t, "json", format)
	})

	t.Run("NewMarshalError", func(t *testing.T) {
		cause := errors.New("marshal failed")
		err := payload.NewMarshalError("json", cause)
		assert.Contains(t, err.Error(), "failed to marshal payload")
		assert.Equal(t, apierror.Internal, err.Kind())
		assert.True(t, errors.Is(err, cause))
		format, _ := err.Details().Get("format")
		assert.Equal(t, "json", format)
		detailCause, _ := err.Details().Get("cause")
		assert.Equal(t, "marshal failed", detailCause)
	})

	t.Run("NewNoTranscodingPathError", func(t *testing.T) {
		err := payload.NewNoTranscodingPathError("json", "golang")
		assert.Contains(t, err.Error(), "no transcoding path found")
		assert.Equal(t, apierror.NotFound, err.Kind())
		from, _ := err.Details().Get("from")
		assert.Equal(t, "json", from)
		to, _ := err.Details().Get("to")
		assert.Equal(t, "golang", to)
	})

	t.Run("NewNoTranscoderError", func(t *testing.T) {
		err := payload.NewNoTranscoderError("json", "golang")
		assert.Contains(t, err.Error(), "no transcoder registered")
		assert.Equal(t, apierror.NotFound, err.Kind())
		from, _ := err.Details().Get("from")
		assert.Equal(t, "json", from)
		to, _ := err.Details().Get("to")
		assert.Equal(t, "golang", to)
	})

	t.Run("NewNoUnmarshalPathError", func(t *testing.T) {
		err := payload.NewNoUnmarshalPathError("json")
		assert.Contains(t, err.Error(), "unmarshaling path")
		assert.Equal(t, apierror.NotFound, err.Kind())
		format, _ := err.Details().Get("format")
		assert.Equal(t, "json", format)
	})

	t.Run("NewInvalidFormatError", func(t *testing.T) {
		err := payload.NewInvalidFormatError("input", "json", "golang")
		assert.Contains(t, err.Error(), "input")
		assert.Equal(t, apierror.Invalid, err.Kind())
		direction, _ := err.Details().Get("direction")
		assert.Equal(t, "input", direction)
		expected, _ := err.Details().Get("expected")
		assert.Equal(t, "json", expected)
		got, _ := err.Details().Get("got")
		assert.Equal(t, "golang", got)
	})

	t.Run("NewInvalidDataTypeError", func(t *testing.T) {
		err := payload.NewInvalidDataTypeError("input", "string", "int")
		assert.Contains(t, err.Error(), "input")
		assert.Contains(t, err.Error(), "invalid data type")
		assert.Equal(t, apierror.Invalid, err.Kind())
		direction, _ := err.Details().Get("direction")
		assert.Equal(t, "input", direction)
		expected, _ := err.Details().Get("expected")
		assert.Equal(t, "string", expected)
		dataType, _ := err.Details().Get("data_type")
		assert.Equal(t, "int", dataType)
	})

	t.Run("NewUnmarshalError", func(t *testing.T) {
		cause := errors.New("unmarshal failed")
		err := payload.NewUnmarshalError("json", cause)
		assert.Contains(t, err.Error(), "failed to unmarshal payload")
		assert.Equal(t, apierror.Invalid, err.Kind())
		assert.True(t, errors.Is(err, cause))
		format, _ := err.Details().Get("format")
		assert.Equal(t, "json", format)
		detailCause, _ := err.Details().Get("cause")
		assert.Equal(t, "unmarshal failed", detailCause)
	})
}
