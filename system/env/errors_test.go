// SPDX-License-Identifier: MPL-2.0

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
		details := err.Details()
		require.NotNil(t, details)
		storageID, _ := details.Get("storage_id")
		assert.Equal(t, "bad-storage", storageID)
	})

	t.Run("NewCreateStorageError", func(t *testing.T) {
		cause := errors.New("permission denied")
		err := NewCreateStorageError(cause)
		assert.Contains(t, err.Error(), "failed to create storage")
		assert.Contains(t, err.Error(), "permission denied")
		assert.Equal(t, cause, errors.Unwrap(err))
		details := err.Details()
		require.NotNil(t, details)
		causeValue, _ := details.Get("cause")
		assert.Equal(t, "permission denied", causeValue)
	})

	t.Run("NewRenameTempFileError", func(t *testing.T) {
		cause := errors.New("access denied")
		err := NewRenameTempFileError(3, cause)
		assert.Contains(t, err.Error(), "failed to rename temp file")
		assert.Equal(t, "False", err.Retryable().String())
		assert.Equal(t, cause, errors.Unwrap(err))
		details := err.Details()
		require.NotNil(t, details)
		attempts, _ := details.Get("attempts")
		assert.Equal(t, 3, attempts)
		causeValue, _ := details.Get("cause")
		assert.Equal(t, "access denied", causeValue)
	})

	t.Run("NewRenameTempFileAfterRemoveError", func(t *testing.T) {
		cause := errors.New("file busy")
		err := NewRenameTempFileAfterRemoveError(cause)
		assert.Contains(t, err.Error(), "failed to rename temp file after removing target")
		assert.Equal(t, cause, errors.Unwrap(err))
		details := err.Details()
		require.NotNil(t, details)
		causeValue, _ := details.Get("cause")
		assert.Equal(t, "file busy", causeValue)
	})

	t.Run("NewVariableNotFoundError", func(t *testing.T) {
		err := NewVariableNotFoundError("MY_VAR")
		assert.Equal(t, "environment variable not found", err.Error())
		assert.Equal(t, "NotFound", err.Kind().String())
		assert.False(t, err.Retryable().Bool())
		details := err.Details()
		require.NotNil(t, details)
		varName, _ := details.Get("variable")
		assert.Equal(t, "MY_VAR", varName)
	})

	t.Run("NewStorageNotFoundError", func(t *testing.T) {
		err := NewStorageNotFoundError("app:my_storage")
		assert.Equal(t, "environment storage backend not found", err.Error())
		assert.Equal(t, "NotFound", err.Kind().String())
		details := err.Details()
		require.NotNil(t, details)
		storageID, _ := details.Get("storage_id")
		assert.Equal(t, "app:my_storage", storageID)
	})

	t.Run("NewInvalidVariableNameError", func(t *testing.T) {
		err := NewInvalidVariableNameError("bad-name", "contains dash")
		assert.Contains(t, err.Error(), "invalid environment variable name")
		assert.Contains(t, err.Error(), "contains dash")
		assert.Equal(t, "Invalid", err.Kind().String())
		details := err.Details()
		require.NotNil(t, details)
		varName, _ := details.Get("variable")
		assert.Equal(t, "bad-name", varName)
		reason, _ := details.Get("reason")
		assert.Equal(t, "contains dash", reason)
	})

	t.Run("NewInvalidVariableError", func(t *testing.T) {
		cause := errors.New("invalid format")
		err := NewInvalidVariableError(cause)
		assert.Contains(t, err.Error(), "invalid variable")
		assert.Equal(t, "Invalid", err.Kind().String())
		assert.Equal(t, cause, errors.Unwrap(err))
	})

	t.Run("NewVariableNameExistsError", func(t *testing.T) {
		err := NewVariableNameExistsError("MY_VAR")
		assert.Contains(t, err.Error(), "variable name already exists")
		assert.Equal(t, "AlreadyExists", err.Kind().String())
		details := err.Details()
		require.NotNil(t, details)
		varName, _ := details.Get("variable")
		assert.Equal(t, "MY_VAR", varName)
	})

	t.Run("NewUnsupportedKindError", func(t *testing.T) {
		err := NewUnsupportedKindError("unknown-kind")
		assert.Contains(t, err.Error(), "unsupported entry kind")
		assert.Equal(t, "Invalid", err.Kind().String())
	})

	t.Run("NewDecodeConfigError", func(t *testing.T) {
		cause := errors.New("json error")
		err := NewDecodeConfigError(cause)
		assert.Contains(t, err.Error(), "failed to decode configuration")
		assert.Contains(t, err.Error(), "json error")
		assert.Equal(t, "Invalid", err.Kind().String())
		assert.Equal(t, cause, errors.Unwrap(err))
		details := err.Details()
		require.NotNil(t, details)
		causeValue, _ := details.Get("cause")
		assert.Equal(t, "json error", causeValue)
	})

	t.Run("NewInvalidConfigError", func(t *testing.T) {
		cause := errors.New("missing field")
		err := NewInvalidConfigError(cause)
		assert.Contains(t, err.Error(), "invalid configuration")
		assert.Contains(t, err.Error(), "missing field")
		assert.Equal(t, cause, errors.Unwrap(err))
		details := err.Details()
		require.NotNil(t, details)
		causeValue, _ := details.Get("cause")
		assert.Equal(t, "missing field", causeValue)
	})

	t.Run("NewStorageNotExistsError", func(t *testing.T) {
		err := NewStorageNotExistsError("app:missing")
		assert.Equal(t, "storage does not exist", err.Error())
		assert.Equal(t, "NotFound", err.Kind().String())
		details := err.Details()
		require.NotNil(t, details)
		storageID, _ := details.Get("storage_id")
		assert.Equal(t, "app:missing", storageID)
	})

	t.Run("NewDecodeVariableError", func(t *testing.T) {
		cause := errors.New("invalid yaml")
		err := NewDecodeVariableError(cause)
		assert.Contains(t, err.Error(), "failed to decode variable")
		assert.Equal(t, cause, errors.Unwrap(err))
	})
}
