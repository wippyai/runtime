package sql

import (
	"errors"
	"testing"

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
