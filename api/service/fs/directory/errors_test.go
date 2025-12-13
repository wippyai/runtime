package directory

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrEmptyDirectoryPath(t *testing.T) {
	err := ErrEmptyDirectoryPath
	assert.Equal(t, "directory path cannot be empty", err.Error())
	assert.Equal(t, "Invalid", err.Kind().String())
	assert.False(t, err.Retryable().Bool())
	assert.Nil(t, err.Details())
	assert.Nil(t, err.Unwrap())
}

func TestNewInvalidModeFormatError(t *testing.T) {
	cause := errors.New("test cause")
	err := NewInvalidModeFormatError(cause)

	require.NotNil(t, err)
	assert.Equal(t, "invalid mode format", err.Error())
	assert.Equal(t, "Invalid", err.Kind().String())
	assert.False(t, err.Retryable().Bool())
	assert.Nil(t, err.Details())
	assert.Equal(t, cause, err.Unwrap())
}
