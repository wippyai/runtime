package fs

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImplementationErrors(t *testing.T) {
	cause := errors.New("test cause")

	t.Run("NewSubscriberError", func(t *testing.T) {
		err := NewSubscriberError(cause)
		assert.Contains(t, err.Error(), "failed to create subscriber")
		assert.Equal(t, "Internal", err.Kind().String())
		assert.True(t, err.Retryable().Bool())
		assert.Equal(t, cause, errors.Unwrap(err))
		require.NotNil(t, err.Details())
	})

	t.Run("NewGetFilesystemError", func(t *testing.T) {
		err := NewGetFilesystemError(cause)
		assert.Equal(t, "failed to get filesystem", err.Error())
		assert.Equal(t, "Internal", err.Kind().String())
		assert.Equal(t, "Unspecified", err.Retryable().String())
	})

	t.Run("NewCreateFilesystemError", func(t *testing.T) {
		err := NewCreateFilesystemError(cause)
		assert.Equal(t, "failed to create filesystem", err.Error())
		assert.Equal(t, cause, errors.Unwrap(err))
	})

	t.Run("NewCreateDirectoryError", func(t *testing.T) {
		err := NewCreateDirectoryError(cause)
		assert.Equal(t, "failed to create directory", err.Error())
		assert.Equal(t, cause, errors.Unwrap(err))
	})

	t.Run("NewOpenDirectoryError", func(t *testing.T) {
		err := NewOpenDirectoryError(cause)
		assert.Equal(t, "failed to open directory", err.Error())
		assert.Equal(t, cause, errors.Unwrap(err))
	})

	t.Run("NewStatError", func(t *testing.T) {
		err := NewStatError(cause)
		assert.Equal(t, "stat failed", err.Error())
		assert.Equal(t, cause, errors.Unwrap(err))
	})

	t.Run("NewOpenError", func(t *testing.T) {
		err := NewOpenError(cause)
		assert.Equal(t, "open failed", err.Error())
		assert.Equal(t, cause, errors.Unwrap(err))
	})

	t.Run("NewGetEmbeddedFilesystemError", func(t *testing.T) {
		err := NewGetEmbeddedFilesystemError(cause)
		assert.Equal(t, "failed to get embedded filesystem", err.Error())
		assert.Equal(t, cause, errors.Unwrap(err))
	})
}
