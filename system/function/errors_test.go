package function

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/registry"
)

func TestErrors(t *testing.T) {
	t.Run("ErrCallCancelled", func(t *testing.T) {
		assert.Equal(t, "async call cancelled", ErrCallCancelled.Error())
		assert.Equal(t, "Canceled", ErrCallCancelled.Kind().String())
		assert.False(t, ErrCallCancelled.Retryable().Bool())
		assert.Nil(t, errors.Unwrap(ErrCallCancelled))
	})

	t.Run("NewInvalidHandlerError", func(t *testing.T) {
		id := registry.NewID("ns", "name")
		err := NewInvalidHandlerError(id)
		assert.Contains(t, err.Error(), "invalid handler type")
		assert.Equal(t, "Internal", err.Kind().String())
	})

	t.Run("NewFrameContextError", func(t *testing.T) {
		cause := errors.New("frame error")
		err := NewFrameContextError(cause)
		assert.Contains(t, err.Error(), "failed to set frame context")
		assert.Equal(t, cause, errors.Unwrap(err))
	})

	t.Run("NewSubscriberError", func(t *testing.T) {
		cause := errors.New("subscriber error")
		err := NewSubscriberError(cause)
		assert.Contains(t, err.Error(), "failed to create subscriber")
		assert.Equal(t, cause, errors.Unwrap(err))
	})
}
