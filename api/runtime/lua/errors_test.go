// SPDX-License-Identifier: MPL-2.0

package lua

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
