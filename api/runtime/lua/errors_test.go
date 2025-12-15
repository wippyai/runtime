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
		{"ErrSourceRequired", ErrSourceRequired, apierror.KindInvalid, "source is required"},
		{"ErrMethodRequired", ErrMethodRequired, apierror.KindInvalid, "method is required"},
		{"ErrEmptyImportAlias", ErrEmptyImportAlias, apierror.KindInvalid, "import alias cannot be empty"},
		{"ErrEmptyModule", ErrEmptyModule, apierror.KindInvalid, "module cannot be empty"},
		{"ErrFSRequired", ErrFSRequired, apierror.KindInvalid, "fs is required"},
		{"ErrPathRequired", ErrPathRequired, apierror.KindInvalid, "path is required"},
		{"ErrHashRequired", ErrHashRequired, apierror.KindInvalid, "hash is required"},
		{"ErrTranscoderNotFound", ErrTranscoderNotFound, apierror.KindNotFound, "transcoder not found in context"},
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
		if err.Kind() != apierror.KindInvalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.KindInvalid)
		}
	})

	t.Run("NewInvalidWorkerPoolSizeError", func(t *testing.T) {
		err := NewInvalidWorkerPoolSizeError()
		if err.Kind() != apierror.KindInvalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.KindInvalid)
		}
	})

	t.Run("NewEmptyImportNameError", func(t *testing.T) {
		err := NewEmptyImportNameError()
		if err.Kind() != apierror.KindInvalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.KindInvalid)
		}
	})

	t.Run("NewModuleNamespaceError", func(t *testing.T) {
		err := NewModuleNamespaceError()
		if err.Kind() != apierror.KindInvalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.KindInvalid)
		}
	})
}

func TestComponentErrorFactories(t *testing.T) {
	t.Run("NewInvalidEntryKindError", func(t *testing.T) {
		err := NewInvalidEntryKindError("process", "function")
		if err.Kind() != apierror.KindInvalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.KindInvalid)
		}
		expected := "invalid entry kind process, expected function"
		if err.Error() != expected {
			t.Errorf("Error() = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("NewValidationError", func(t *testing.T) {
		cause := errors.New("missing required field")
		err := NewValidationError(cause)
		if err.Kind() != apierror.KindInvalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.KindInvalid)
		}
		expected := "invalid configuration: missing required field"
		if err.Error() != expected {
			t.Errorf("Error() = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("NewPoolNotFoundError", func(t *testing.T) {
		err := NewPoolNotFoundError("app.functions.myFunc")
		if err.Kind() != apierror.KindNotFound {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.KindNotFound)
		}
		expected := "pool not found: app.functions.myFunc"
		if err.Error() != expected {
			t.Errorf("Error() = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("NewUnknownPoolTypeError", func(t *testing.T) {
		err := NewUnknownPoolTypeError("distributed")
		if err.Kind() != apierror.KindInvalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.KindInvalid)
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
		if err.Kind() != apierror.KindInvalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.KindInvalid)
		}
	})

	t.Run("NewFilesystemNotFoundError", func(t *testing.T) {
		err := NewFilesystemNotFoundError("my-fs")
		if err.Kind() != apierror.KindNotFound {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.KindNotFound)
		}
		expected := "filesystem not found: my-fs"
		if err.Error() != expected {
			t.Errorf("Error() = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("NewOpenFileError", func(t *testing.T) {
		cause := errors.New("permission denied")
		err := NewOpenFileError("/path/to/file.lua", cause)
		if err.Kind() != apierror.KindNotFound {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.KindNotFound)
		}
		expected := "failed to open file: /path/to/file.lua"
		if err.Error() != expected {
			t.Errorf("Error() = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("NewInvalidHashFormatError", func(t *testing.T) {
		err := NewInvalidHashFormatError("invalid")
		if err.Kind() != apierror.KindInvalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.KindInvalid)
		}
	})

	t.Run("NewUnsupportedHashAlgorithmError", func(t *testing.T) {
		err := NewUnsupportedHashAlgorithmError("md5")
		if err.Kind() != apierror.KindInvalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.KindInvalid)
		}
	})

	t.Run("NewHashMismatchError", func(t *testing.T) {
		err := NewHashMismatchError("abc123", "def456")
		if err.Kind() != apierror.KindInvalid {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.KindInvalid)
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
