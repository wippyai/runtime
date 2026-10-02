// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"errors"
	"testing"

	lua "github.com/wippyai/go-lua"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestErrorError(t *testing.T) {
	err := &Error{
		message: "test error",
	}

	if err.Error() != "test error" {
		t.Errorf("expected 'test error', got %s", err.Error())
	}
}

func TestErrorErrorWithCause(t *testing.T) {
	cause := errors.New("underlying error")
	err := &Error{
		message: "wrapper error",
		cause:   cause,
	}

	expected := "wrapper error: underlying error"
	if err.Error() != expected {
		t.Errorf("expected %s, got %s", expected, err.Error())
	}
}

func TestErrorKind(t *testing.T) {
	err := &Error{
		kind: apierror.Invalid,
	}

	if err.Kind() != apierror.Invalid {
		t.Errorf("expected Invalid, got %v", err.Kind())
	}
}

func TestErrorRetryable(t *testing.T) {
	err := &Error{
		retryable: apierror.False,
	}

	if err.Retryable() != apierror.False {
		t.Errorf("expected False, got %v", err.Retryable())
	}
}

func TestErrorUnwrap(t *testing.T) {
	cause := errors.New("cause")
	err := &Error{
		cause: cause,
	}

	if !errors.Is(errors.Unwrap(err), cause) {
		t.Error("expected same cause error")
	}
}

func TestNewInvalidParametersTypeError(t *testing.T) {
	err := NewInvalidParametersTypeError("string")

	if err.Kind() != apierror.Invalid {
		t.Errorf("expected Invalid, got %v", err.Kind())
	}

	if err.Retryable() != apierror.False {
		t.Errorf("expected False, got %v", err.Retryable())
	}

	expectedMsg := "parameters must be a table, got string"
	if err.Error() != expectedMsg {
		t.Errorf("expected %s, got %s", expectedMsg, err.Error())
	}
}

func TestSQLWrappingPreservesNonSQLiteMetadata(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	cause := lua.NewError("driver error").WithKind(lua.Unavailable).WithRetryable(true).
		WithDetails(map[string]any{"operation_id": "original"})
	wrapped := wrapSQLError(l, cause, "query")
	if wrapped.Kind() != lua.Unavailable || wrapped.Retryable() != lua.TernaryTrue || wrapped.Details()["operation_id"] != "original" {
		t.Fatalf("lost metadata: %v", wrapped)
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("original cause was lost")
	}
	if _, ok := wrapped.Details()["sqlite_code"]; ok {
		t.Fatal("non-SQLite error acquired SQLite code")
	}
	if _, ok := wrapped.Details()["sqlite_extended_code"]; ok {
		t.Fatal("non-SQLite error acquired extended SQLite code")
	}
	wrapped.Details()["operation_id"] = "changed"
	if cause.Details()["operation_id"] != "original" {
		t.Fatal("input metadata mutated")
	}
}

func TestSQLWrappingNilError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	if wrapped := wrapSQLError(l, nil, "query"); wrapped != nil {
		t.Fatalf("nil cause acquired an error: %v", wrapped)
	}
}
