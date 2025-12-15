package lua

import (
	"errors"
	"testing"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     apierror.Error
		kind    apierror.Kind
		message string
	}{
		{"ErrProcessNotInitialized", ErrProcessNotInitialized, apierror.Internal, "process not initialized"},
		{"ErrProcessContextNotAvailable", ErrProcessContextNotAvailable, apierror.Internal, "process context not available"},
		{"ErrStateNotInitialized", ErrStateNotInitialized, apierror.Internal, "process state not initialized - use Factory.Create()"},
		{"ErrCouldNotAccessRegistry", ErrCouldNotAccessRegistry, apierror.Internal, "could not access registry"},
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

func TestImplementationErrorFactories(t *testing.T) {
	t.Run("NewUnpackConfigError", func(t *testing.T) {
		cause := errors.New("json parse error")
		err := NewUnpackConfigError("function", cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
		expected := "failed to unpack function config"
		if err.Error() != expected {
			t.Errorf("Error() = %q, want %q", err.Error(), expected)
		}
		if !errors.Is(err, cause) {
			t.Error("error should wrap cause")
		}
	})

	t.Run("NewUnmarshalConfigError", func(t *testing.T) {
		cause := errors.New("invalid json")
		err := NewUnmarshalConfigError(cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
	})

	t.Run("NewCompileError", func(t *testing.T) {
		cause := errors.New("syntax error")
		err := NewCompileError(cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
	})

	t.Run("NewAddNodeError", func(t *testing.T) {
		cause := errors.New("duplicate key")
		err := NewAddNodeError("function", cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
		expected := "failed to add function node"
		if err.Error() != expected {
			t.Errorf("Error() = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("NewUpdateNodeError", func(t *testing.T) {
		cause := errors.New("not found")
		err := NewUpdateNodeError("process", cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
	})

	t.Run("NewDeleteNodeError", func(t *testing.T) {
		cause := errors.New("in use")
		err := NewDeleteNodeError("workflow", cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
	})

	t.Run("NewRegisterFactoryError", func(t *testing.T) {
		cause := errors.New("duplicate registration")
		err := NewRegisterFactoryError(cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
	})

	t.Run("NewUpdateFactoryError", func(t *testing.T) {
		cause := errors.New("factory not found")
		err := NewUpdateFactoryError(cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
	})

	t.Run("NewCreatePoolError", func(t *testing.T) {
		cause := errors.New("resource exhausted")
		err := NewCreatePoolError(cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
	})

	t.Run("NewReplacePoolError", func(t *testing.T) {
		cause := errors.New("pool busy")
		err := NewReplacePoolError(cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
	})

	t.Run("NewModuleInitError", func(t *testing.T) {
		cause := errors.New("module error")
		err := NewModuleInitError("sql", cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
		expected := "failed to initialize module: sql"
		if err.Error() != expected {
			t.Errorf("Error() = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("NewRegisterCallerError", func(t *testing.T) {
		id := registry.NewID("app", "myFunc")
		cause := errors.New("register failed")
		err := NewRegisterCallerError(&id, cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
	})

	t.Run("NewUnregisterCallerError", func(t *testing.T) {
		id := registry.NewID("app", "myFunc")
		cause := errors.New("unregister failed")
		err := NewUnregisterCallerError(&id, cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
	})

	t.Run("NewLoadBytecodeError", func(t *testing.T) {
		cause := errors.New("file not found")
		err := NewLoadBytecodeError(cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
	})

	t.Run("NewUndumpBytecodeError", func(t *testing.T) {
		cause := errors.New("invalid bytecode")
		err := NewUndumpBytecodeError(cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
	})

	t.Run("NewStoreResourcesError", func(t *testing.T) {
		cause := errors.New("store failed")
		err := NewStoreResourcesError(cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
	})

	t.Run("NewLoadScriptError", func(t *testing.T) {
		cause := errors.New("parse error")
		err := NewLoadScriptError(cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
	})

	t.Run("NewExecuteScriptError", func(t *testing.T) {
		cause := errors.New("runtime error")
		err := NewExecuteScriptError(cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
	})

	t.Run("NewOperationError", func(t *testing.T) {
		cause := errors.New("operation failed")
		err := NewOperationError("custom operation", cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
		if err.Error() != "custom operation" {
			t.Errorf("Error() = %q, want %q", err.Error(), "custom operation")
		}
	})

	t.Run("NewRuntimeError", func(t *testing.T) {
		err := NewRuntimeError("runtime failure")
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
		if err.Error() != "runtime failure" {
			t.Errorf("Error() = %q, want %q", err.Error(), "runtime failure")
		}
	})

	t.Run("NewRegistryTableError", func(t *testing.T) {
		cause := errors.New("table access failed")
		err := NewRegistryTableError(cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
	})

	t.Run("NewRegistryAddError", func(t *testing.T) {
		cause := errors.New("add failed")
		err := NewRegistryAddError(cause)
		if err.Kind() != apierror.Internal {
			t.Errorf("Kind() = %v, want %v", err.Kind(), apierror.Internal)
		}
	})
}

func TestErrorUnwrap(t *testing.T) {
	root := errors.New("root cause")
	err := NewCompileError(root)

	if !errors.Is(err, root) {
		t.Error("errors.Is should find root cause")
	}

	if !errors.Is(errors.Unwrap(err), root) {
		t.Errorf("Unwrap() = %v, want %v", errors.Unwrap(err), root)
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
