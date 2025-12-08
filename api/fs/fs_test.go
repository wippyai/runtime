// Package fs provides filesystem abstractions and a registry.
package fs

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
)

func TestEventConstants(t *testing.T) {
	tests := []struct {
		name     string
		system   event.System
		kind     event.Kind
		expected string
	}{
		{"system", System, "", "fs"},
		{"register", "", Register, "fs.register"},
		{"delete", "", Delete, "fs.delete"},
		{"accept", "", Accept, "fs.accept"},
		{"reject", "", Reject, "fs.reject"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.system != "" {
				assert.Equal(t, tt.expected, tt.system)
			}
			if tt.kind != "" {
				assert.Equal(t, tt.expected, tt.kind)
			}
		})
	}
}

func TestContext_Registry(t *testing.T) {
	t.Run("with app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		reg := GetRegistry(ctx)
		assert.Nil(t, reg)

		type mockRegistry struct{ Registry }
		mockReg := &mockRegistry{}

		ctx = WithRegistry(ctx, mockReg)

		retrieved := GetRegistry(ctx)
		assert.Equal(t, mockReg, retrieved)
	})

	t.Run("without app context", func(t *testing.T) {
		ctx := context.Background()

		reg := GetRegistry(ctx)
		assert.Nil(t, reg)

		type mockRegistry struct{ Registry }
		mockReg := &mockRegistry{}

		ctx = WithRegistry(ctx, mockReg)
		assert.Equal(t, context.Background(), ctx)

		reg = GetRegistry(ctx)
		assert.Nil(t, reg)
	})
}

func TestErrorInterface(t *testing.T) {
	cause := errors.New("test cause")

	t.Run("NewSubscriberError", func(t *testing.T) {
		err := NewSubscriberError(cause)
		assert.Contains(t, err.Error(), "failed to create subscriber")
		assert.Equal(t, "Internal", err.Kind().String())
		assert.True(t, err.Retryable().Bool())
		assert.Equal(t, cause, err.Unwrap())
		require.NotNil(t, err.Details())
	})

	t.Run("NewUnsupportedEntryKindError", func(t *testing.T) {
		err := NewUnsupportedEntryKindError("unknown")
		assert.Contains(t, err.Error(), "unsupported entry kind")
		assert.Equal(t, "Invalid", err.Kind().String())
		details := err.Details()
		require.NotNil(t, details)
		kind, _ := details.Get("kind")
		assert.Equal(t, "unknown", kind)
	})

	t.Run("NewDecodeConfigError", func(t *testing.T) {
		err := NewDecodeConfigError(cause)
		assert.Equal(t, "failed to decode config", err.Error())
		assert.Equal(t, "Invalid", err.Kind().String())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("NewFilesystemAlreadyExistsError", func(t *testing.T) {
		err := NewFilesystemAlreadyExistsError("test-fs")
		assert.Contains(t, err.Error(), "test-fs")
		assert.Contains(t, err.Error(), "already exists")
		assert.Equal(t, "AlreadyExists", err.Kind().String())
	})

	t.Run("NewFilesystemNotFoundError", func(t *testing.T) {
		err := NewFilesystemNotFoundError("test-fs")
		assert.Contains(t, err.Error(), "test-fs")
		assert.Contains(t, err.Error(), "not found")
		assert.Equal(t, "NotFound", err.Kind().String())
	})

	t.Run("NewFilesystemNotFoundWithCauseError", func(t *testing.T) {
		err := NewFilesystemNotFoundWithCauseError("test-fs", cause)
		assert.Contains(t, err.Error(), "filesystem not found")
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("NewGetFilesystemError", func(t *testing.T) {
		err := NewGetFilesystemError(cause)
		assert.Equal(t, "failed to get filesystem", err.Error())
		assert.Equal(t, "Internal", err.Kind().String())
		assert.Equal(t, "Unknown", err.Retryable().String())
	})

	t.Run("NewCreateFilesystemError", func(t *testing.T) {
		err := NewCreateFilesystemError(cause)
		assert.Equal(t, "failed to create filesystem", err.Error())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("NewInvalidPathError", func(t *testing.T) {
		err := NewInvalidPathError(cause)
		assert.Equal(t, "invalid path", err.Error())
		assert.Equal(t, "Invalid", err.Kind().String())
	})

	t.Run("NewCreateDirectoryError", func(t *testing.T) {
		err := NewCreateDirectoryError(cause)
		assert.Equal(t, "failed to create directory", err.Error())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("NewOpenDirectoryError", func(t *testing.T) {
		err := NewOpenDirectoryError(cause)
		assert.Equal(t, "failed to open directory", err.Error())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("NewUnsupportedOperationError", func(t *testing.T) {
		err := NewUnsupportedOperationError("truncate")
		assert.Contains(t, err.Error(), "truncate")
		assert.Contains(t, err.Error(), "operation not supported")
		details := err.Details()
		require.NotNil(t, details)
		op, _ := details.Get("operation")
		assert.Equal(t, "truncate", op)
	})

	t.Run("NewEmptyPathError", func(t *testing.T) {
		err := NewEmptyPathError()
		assert.Equal(t, "path cannot be empty", err.Error())
		assert.Equal(t, "Invalid", err.Kind().String())
	})

	t.Run("NewNilReaderError", func(t *testing.T) {
		err := NewNilReaderError()
		assert.Equal(t, "reader cannot be nil", err.Error())
		assert.Equal(t, "Invalid", err.Kind().String())
	})

	t.Run("NewStatError", func(t *testing.T) {
		err := NewStatError(cause)
		assert.Equal(t, "stat failed", err.Error())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("NewOpenError", func(t *testing.T) {
		err := NewOpenError(cause)
		assert.Equal(t, "open failed", err.Error())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("NewGetEmbeddedFilesystemError", func(t *testing.T) {
		err := NewGetEmbeddedFilesystemError(cause)
		assert.Equal(t, "failed to get embedded filesystem", err.Error())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("NewEmptyPackPathError", func(t *testing.T) {
		err := NewEmptyPackPathError()
		assert.Equal(t, "packPath cannot be empty", err.Error())
		assert.Equal(t, "Invalid", err.Kind().String())
	})

	t.Run("NewReadOnlyOperationError", func(t *testing.T) {
		err := NewReadOnlyOperationError("write")
		assert.Contains(t, err.Error(), "write")
		assert.Contains(t, err.Error(), "read-only filesystem")
	})

	t.Run("NewPermissionDeniedError", func(t *testing.T) {
		err := NewPermissionDeniedError(0o644, 0o755, cause)
		assert.Equal(t, "permission denied", err.Error())
		assert.Equal(t, "PermissionDenied", err.Kind().String())
		assert.Equal(t, cause, err.Unwrap())
		details := err.Details()
		require.NotNil(t, details)
	})
}
