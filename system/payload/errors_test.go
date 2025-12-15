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
		assert.Contains(t, err.Error(), "transcoding")
		assert.Equal(t, apierror.Internal, err.Kind())
		assert.True(t, errors.Is(err, cause))
	})

	t.Run("NewUnmarshalTranscodeError", func(t *testing.T) {
		cause := errors.New("test error")
		err := payload.NewUnmarshalTranscodeError(cause)
		assert.Contains(t, err.Error(), "transcoding payload")
		assert.Equal(t, apierror.Internal, err.Kind())
		assert.True(t, errors.Is(err, cause))
	})

	t.Run("NewUnmarshalerNotFoundError", func(t *testing.T) {
		err := payload.NewUnmarshalerNotFoundError("json")
		assert.Contains(t, err.Error(), "unmarshaler not found")
		assert.Equal(t, apierror.Internal, err.Kind())
	})

	t.Run("NewMarshalError", func(t *testing.T) {
		cause := errors.New("marshal failed")
		err := payload.NewMarshalError("json", cause)
		assert.Contains(t, err.Error(), "marshal")
		assert.Contains(t, err.Error(), "json")
		assert.Equal(t, apierror.Internal, err.Kind())
		assert.True(t, errors.Is(err, cause))
	})

	t.Run("NewNoTranscodingPathError", func(t *testing.T) {
		err := payload.NewNoTranscodingPathError("json", "golang")
		assert.Contains(t, err.Error(), "json")
		assert.Contains(t, err.Error(), "golang")
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
	})

	t.Run("NewNoUnmarshalPathError", func(t *testing.T) {
		err := payload.NewNoUnmarshalPathError("json")
		assert.Contains(t, err.Error(), "unmarshaling path")
		assert.Equal(t, apierror.NotFound, err.Kind())
	})

	t.Run("NewInvalidFormatError", func(t *testing.T) {
		err := payload.NewInvalidFormatError("input", "json", "golang")
		assert.Contains(t, err.Error(), "input")
		assert.Equal(t, apierror.Invalid, err.Kind())
		direction, _ := err.Details().Get("direction")
		assert.Equal(t, "input", direction)
	})

	t.Run("NewInvalidDataTypeError", func(t *testing.T) {
		err := payload.NewInvalidDataTypeError("input", "string", "int")
		assert.Contains(t, err.Error(), "input")
		assert.Contains(t, err.Error(), "string")
		assert.Equal(t, apierror.Invalid, err.Kind())
	})

	t.Run("NewUnmarshalError", func(t *testing.T) {
		cause := errors.New("unmarshal failed")
		err := payload.NewUnmarshalError("json", cause)
		assert.Contains(t, err.Error(), "unmarshal")
		assert.Contains(t, err.Error(), "json")
		assert.Equal(t, apierror.Invalid, err.Kind())
		assert.True(t, errors.Is(err, cause))
	})
}
