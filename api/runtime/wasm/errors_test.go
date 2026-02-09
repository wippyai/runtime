package wasm

import (
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
		{"ErrEmptyImportName", ErrEmptyImportName, apierror.Invalid, "import :name cannot be empty"},
		{"ErrFSRequired", ErrFSRequired, apierror.Invalid, "fs is required"},
		{"ErrPathRequired", ErrPathRequired, apierror.Invalid, "path is required"},
		{"ErrHashRequired", ErrHashRequired, apierror.Invalid, "hash is required"},
		{"ErrInvalidPoolType", ErrInvalidPoolType, apierror.Invalid, "invalid pool type"},
		{"ErrInvalidPoolSize", ErrInvalidPoolSize, apierror.Invalid, "pool.size must be greater than 0 for non-flex pools"},
		{"ErrInvalidWorkerPoolSize", ErrInvalidWorkerPoolSize, apierror.Invalid, "pool.size must be greater than 0 for worker pools"},
		{"ErrInvalidPoolConfig", ErrInvalidPoolConfig, apierror.Invalid, "pool values cannot be negative"},
		{"ErrInvalidTransportType", ErrInvalidTransportType, apierror.Invalid, "invalid transport type"},
		{"ErrInvalidExecutionLimit", ErrInvalidExecutionLimit, apierror.Invalid, "limits.max_execution_ms cannot be negative"},
		{"ErrWASICwdMustBeAbsolute", ErrWASICwdMustBeAbsolute, apierror.Invalid, "wasi.cwd must be absolute"},
		{"ErrWASIEnvIDRequired", ErrWASIEnvIDRequired, apierror.Invalid, "wasi.env[].id is required"},
		{"ErrWASIEnvNameRequired", ErrWASIEnvNameRequired, apierror.Invalid, "wasi.env[].name is required"},
		{"ErrWASIEnvNameDuplicate", ErrWASIEnvNameDuplicate, apierror.Invalid, "wasi.env[].name must be unique"},
		{"ErrWASIMountFSRequired", ErrWASIMountFSRequired, apierror.Invalid, "wasi.mounts[].fs is required"},
		{"ErrWASIMountGuestRequired", ErrWASIMountGuestRequired, apierror.Invalid, "wasi.mounts[].guest is required"},
		{"ErrWASIMountGuestMustBeAbsolute", ErrWASIMountGuestMustBeAbsolute, apierror.Invalid, "wasi.mounts[].guest must be absolute"},
		{"ErrWASIMountGuestDuplicate", ErrWASIMountGuestDuplicate, apierror.Invalid, "wasi.mounts[].guest must be unique"},
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
