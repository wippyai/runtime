package env

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrors(t *testing.T) {
	t.Run("NewSubscriberError", func(t *testing.T) {
		cause := errors.New("connection failed")
		err := NewSubscriberError(cause)
		assert.Contains(t, err.Error(), "failed to create subscriber")
		assert.Contains(t, err.Error(), "connection failed")
		assert.Equal(t, "Internal", err.Kind().String())
		assert.True(t, err.Retryable().Bool())
		assert.Equal(t, cause, errors.Unwrap(err))
	})

	t.Run("NewInvalidStorageTypeError", func(t *testing.T) {
		err := NewInvalidStorageTypeError("bad-storage")
		assert.Contains(t, err.Error(), "invalid storage type")
		assert.Equal(t, "Internal", err.Kind().String())
	})

	t.Run("NewCreateStorageError", func(t *testing.T) {
		cause := errors.New("permission denied")
		err := NewCreateStorageError(cause)
		assert.Equal(t, "failed to create storage", err.Error())
		assert.Equal(t, cause, errors.Unwrap(err))
	})

	t.Run("NewRenameTempFileError", func(t *testing.T) {
		cause := errors.New("access denied")
		err := NewRenameTempFileError(3, cause)
		assert.Contains(t, err.Error(), "failed to rename temp file")
		assert.Equal(t, "Unknown", err.Retryable().String())
		assert.Equal(t, cause, errors.Unwrap(err))
		details := err.Details()
		require.NotNil(t, details)
		attempts, _ := details.Get("attempts")
		assert.Equal(t, 3, attempts)
	})

	t.Run("NewRenameTempFileAfterRemoveError", func(t *testing.T) {
		cause := errors.New("file busy")
		err := NewRenameTempFileAfterRemoveError(cause)
		assert.Contains(t, err.Error(), "failed to rename temp file after removing target")
		assert.Equal(t, cause, errors.Unwrap(err))
	})
}
