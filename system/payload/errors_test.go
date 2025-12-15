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
}
