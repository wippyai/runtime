package lua

import (
	"errors"
	"testing"

	apierror "github.com/wippyai/runtime/api/error"
)

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     apierror.Error
		kind    apierror.Kind
		message string
	}{
		{"ErrSourceRequired", ErrSourceRequired, apierror.Invalid, "source is required"},
		{"ErrMethodRequired", ErrMethodRequired, apierror.Invalid, "method is required"},
		{"ErrEmptyImportAlias", ErrEmptyImportAlias, apierror.Invalid, "import alias cannot be empty"},
		{"ErrEmptyModule", ErrEmptyModule, apierror.Invalid, "module cannot be empty"},
		{"ErrFSRequired", ErrFSRequired, apierror.Invalid, "fs is required"},
		{"ErrPathRequired", ErrPathRequired, apierror.Invalid, "path is required"},
		{"ErrHashRequired", ErrHashRequired, apierror.Invalid, "hash is required"},
		{"ErrTranscoderNotFound", ErrTranscoderNotFound, apierror.NotFound, "transcoder not found in context"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Kind() != tt.kind {
				t.Errorf("Kind() = %v, want %v", tt.err.Kind(), tt.kind)
			}
			if tt.err.Error() != tt.message {
				t.Errorf("Error() = %q, want %q", tt.err.Error(), tt.message)
			}
			if tt.err.Retryable() != apierror.False {
				t.Errorf("Retryable() = %v, want %v", tt.err.Retryable(), apierror.False)
			}
		})
	}
}

func TestConfigErrorFactories(t *testing.T) {
	t.Run("NewInvalidPoolSizeError", func(t *testing.T) {
		err := NewInvalidPoolSizeError()
		if err.Kind() != apierror.Invalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Invalid)
		}
	})

	t.Run("NewInvalidWorkerPoolSizeError", func(t *testing.T) {
		err := NewInvalidWorkerPoolSizeError()
		if err.Kind() != apierror.Invalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Invalid)
		}
	})

	t.Run("NewEmptyImportNameError", func(t *testing.T) {
		err := NewEmptyImportNameError()
		if err.Kind() != apierror.Invalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Invalid)
		}
	})

	t.Run("NewModuleNamespaceError", func(t *testing.T) {
		err := NewModuleNamespaceError()
		if err.Kind() != apierror.Invalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Invalid)
		}
	})
}

func TestComponentErrorFactories(t *testing.T) {
	t.Run("NewInvalidEntryKindError", func(t *testing.T) {
		err := NewInvalidEntryKindError("process", "function")
		if err.Kind() != apierror.Invalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Invalid)
		}
		expected := "invalid entry kind process, expected function"
		if err.Error() != expected {
			t.Errorf("Error() = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("NewValidationError", func(t *testing.T) {
		cause := errors.New("missing required field")
		err := NewValidationError(cause)
		if err.Kind() != apierror.Invalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Invalid)
		}
		expected := "invalid configuration: missing required field"
		if err.Error() != expected {
			t.Errorf("Error() = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("NewPoolNotFoundError", func(t *testing.T) {
		err := NewPoolNotFoundError("app.functions.myFunc")
		if err.Kind() != apierror.NotFound {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.NotFound)
		}
		expected := "pool not found: app.functions.myFunc"
		if err.Error() != expected {
			t.Errorf("Error() = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("NewUnknownPoolTypeError", func(t *testing.T) {
		err := NewUnknownPoolTypeError("distributed")
		if err.Kind() != apierror.Invalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Invalid)
		}
		expected := "unknown pool type: distributed"
		if err.Error() != expected {
			t.Errorf("Error() = %q, want %q", err.Error(), expected)
		}
	})
}

func TestBytecodeErrorFactories(t *testing.T) {
	t.Run("NewHashVerificationError", func(t *testing.T) {
		cause := errors.New("hash mismatch")
		err := NewHashVerificationError(cause)
		if err.Kind() != apierror.Invalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Invalid)
		}
	})

	t.Run("NewFilesystemNotFoundError", func(t *testing.T) {
		err := NewFilesystemNotFoundError("my-fs")
		if err.Kind() != apierror.NotFound {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.NotFound)
		}
		expected := "filesystem not found: my-fs"
		if err.Error() != expected {
			t.Errorf("Error() = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("NewOpenFileError", func(t *testing.T) {
		cause := errors.New("permission denied")
		err := NewOpenFileError("/path/to/file.lua", cause)
		if err.Kind() != apierror.NotFound {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.NotFound)
		}
		expected := "failed to open file: /path/to/file.lua"
		if err.Error() != expected {
			t.Errorf("Error() = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("NewInvalidHashFormatError", func(t *testing.T) {
		err := NewInvalidHashFormatError("invalid")
		if err.Kind() != apierror.Invalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Invalid)
		}
	})

	t.Run("NewUnsupportedHashAlgorithmError", func(t *testing.T) {
		err := NewUnsupportedHashAlgorithmError("md5")
		if err.Kind() != apierror.Invalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Invalid)
		}
	})

	t.Run("NewHashMismatchError", func(t *testing.T) {
		err := NewHashMismatchError("abc123", "def456")
		if err.Kind() != apierror.Invalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Invalid)
		}
		expected := "hash mismatch: expected abc123, got def456"
		if err.Error() != expected {
			t.Errorf("Error() = %q, want %q", err.Error(), expected)
		}
	})
}

func TestErrorUnwrap(t *testing.T) {
	root := errors.New("root cause")
	err := NewHashVerificationError(root)

	if !errors.Is(err, root) {
		t.Error("errors.Is should find root cause")
	}

	if !errors.Is(errors.Unwrap(err), root) {
		t.Errorf("Unwrap() = %v, want %v", errors.Unwrap(err), root)
	}
}
