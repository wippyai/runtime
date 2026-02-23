// SPDX-License-Identifier: MPL-2.0

// Package error provides error categorization and retry metadata.
package error

import (
	"errors"
	"testing"

	"github.com/wippyai/runtime/api/attrs"
)

func TestNew(t *testing.T) {
	e := New(NotFound, "resource not found")

	if e.Kind() != NotFound {
		t.Errorf("Kind() = %v, want %v", e.Kind(), NotFound)
	}
	if e.Error() != "resource not found" {
		t.Errorf("Error() = %v, want %v", e.Error(), "resource not found")
	}
	if e.Retryable() != Unspecified {
		t.Errorf("Retryable() = %v, want %v", e.Retryable(), Unspecified)
	}
	if e.Details() != nil {
		t.Errorf("Details() = %v, want nil", e.Details())
	}
}

func TestBuilder_WithRetryable(t *testing.T) {
	e := New(Unavailable, "service down").WithRetryable(True)

	if e.Retryable() != True {
		t.Errorf("Retryable() = %v, want %v", e.Retryable(), True)
	}
}

func TestBuilder_WithDetails(t *testing.T) {
	details := attrs.NewBagFrom(map[string]any{"key": "value"})
	e := New(Invalid, "validation failed").WithDetails(details)

	if e.Details() == nil {
		t.Error("Details() should not be nil")
	}
}

func TestBuilder_WithCause(t *testing.T) {
	cause := errors.New("underlying error")
	e := New(Internal, "operation failed").WithCause(cause)

	if !errors.Is(e, cause) {
		t.Errorf("errors.Is(e, cause) = false, want true")
	}
}

func TestBuilder_WithMessage(t *testing.T) {
	e := New(NotFound, "original").WithMessage("updated message")

	if e.Error() != "updated message" {
		t.Errorf("Error() = %v, want %v", e.Error(), "updated message")
	}
}

func TestBuilder_Chaining(t *testing.T) {
	cause := errors.New("cause")
	details := attrs.NewBagFrom(map[string]any{"code": 123})

	e := New(Conflict, "conflict").
		WithRetryable(False).
		WithDetails(details).
		WithCause(cause).
		WithMessage("final message")

	if e.Kind() != Conflict {
		t.Errorf("Kind() = %v, want %v", e.Kind(), Conflict)
	}
	if e.Error() != "final message: cause" {
		t.Errorf("Error() = %v, want %v", e.Error(), "final message: cause")
	}
	if e.Retryable() != False {
		t.Errorf("Retryable() = %v, want %v", e.Retryable(), False)
	}
	if !errors.Is(e, cause) {
		t.Errorf("errors.Is(e, cause) = false, want true")
	}
}

func TestE(t *testing.T) {
	cause := errors.New("cause")
	details := attrs.NewBagFrom(map[string]any{"field": "name"})

	e := E(Invalid, "validation error", False, details, cause)

	if e.Kind() != Invalid {
		t.Errorf("Kind() = %v, want %v", e.Kind(), Invalid)
	}
	if e.Error() != "validation error: cause" {
		t.Errorf("Error() = %v, want %v", e.Error(), "validation error: cause")
	}
	if e.Retryable() != False {
		t.Errorf("Retryable() = %v, want %v", e.Retryable(), False)
	}
	if e.Details() == nil {
		t.Error("Details() should not be nil")
	}
}

func TestWithDetails(t *testing.T) {
	details := attrs.NewBagFrom(map[string]any{"id": 42})
	e := WithDetails(NotFound, "not found", details)

	if e.Kind() != NotFound {
		t.Errorf("Kind() = %v, want %v", e.Kind(), NotFound)
	}
	if e.Details() == nil {
		t.Error("Details() should not be nil")
	}
	if e.Retryable() != Unspecified {
		t.Errorf("Retryable() = %v, want %v", e.Retryable(), Unspecified)
	}
}

func TestSetCause(t *testing.T) {
	original := New(Internal, "original error").WithRetryable(True)
	cause := errors.New("new cause")

	e := SetCause(original, cause)

	if e.Kind() != Internal {
		t.Errorf("Kind() = %v, want %v", e.Kind(), Internal)
	}
	if e.Retryable() != True {
		t.Errorf("Retryable() = %v, want %v", e.Retryable(), True)
	}
	if !errors.Is(e, cause) {
		t.Errorf("errors.Is(e, cause) = false, want true")
	}
}

func TestSetMessage(t *testing.T) {
	original := New(Timeout, "original").WithRetryable(True)
	e := SetMessage(original, "new message")

	if e.Kind() != Timeout {
		t.Errorf("Kind() = %v, want %v", e.Kind(), Timeout)
	}
	if e.Error() != "new message" {
		t.Errorf("Error() = %v, want %v", e.Error(), "new message")
	}
	if e.Retryable() != True {
		t.Errorf("Retryable() = %v, want %v", e.Retryable(), True)
	}
}

func TestSetMessage_PreservesCause(t *testing.T) {
	cause := errors.New("original cause")
	original := New(Internal, "msg").WithCause(cause)
	e := SetMessage(original, "new message")

	if !errors.Is(e, cause) {
		t.Errorf("errors.Is(e, cause) = false, want true")
	}
}

func TestSetDetails(t *testing.T) {
	original := New(Invalid, "error").WithRetryable(False)
	details := attrs.NewBagFrom(map[string]any{"field": "email"})

	e := SetDetails(original, details)

	if e.Kind() != Invalid {
		t.Errorf("Kind() = %v, want %v", e.Kind(), Invalid)
	}
	if e.Details() == nil {
		t.Error("Details() should not be nil")
	}
	// SetDetails wraps original as cause
	if errors.Unwrap(e) == nil {
		t.Error("SetDetails should wrap original as cause")
	}
}

func TestError_Is(t *testing.T) {
	e1 := New(NotFound, "user not found")
	e2 := New(NotFound, "user not found")
	e3 := New(NotFound, "different message")
	e4 := New(Invalid, "user not found")

	if !errors.Is(e1, e2) {
		t.Error("errors.Is should match same kind and message")
	}
	if errors.Is(e1, e3) {
		t.Error("errors.Is should not match different message")
	}
	if errors.Is(e1, e4) {
		t.Error("errors.Is should not match different kind")
	}
}

func TestError_Is_WithStandardError(t *testing.T) {
	e := New(Internal, "error")
	stdErr := errors.New("standard error")

	if errors.Is(e, stdErr) {
		t.Error("errors.Is should not match standard error")
	}
}

func TestKind_String(t *testing.T) {
	tests := []struct {
		kind Kind
		want string
	}{
		{Unknown, "Unknown"},
		{NotFound, "NotFound"},
		{AlreadyExists, "AlreadyExists"},
		{Invalid, "Invalid"},
		{PermissionDenied, "PermissionDenied"},
		{Unavailable, "Unavailable"},
		{Internal, "Internal"},
		{Canceled, "Canceled"},
		{Conflict, "Conflict"},
		{Timeout, "Timeout"},
		{RateLimited, "RateLimited"},
		{Kind("custom_kind"), "custom_kind"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.want {
				t.Errorf("Kind.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTernary_String(t *testing.T) {
	tests := []struct {
		want    string
		ternary Ternary
	}{
		{"Unspecified", Unspecified},
		{"True", True},
		{"False", False},
		{"Unspecified", Ternary(999)},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.ternary.String(); got != tt.want {
				t.Errorf("Ternary.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTernary_Bool(t *testing.T) {
	tests := []struct {
		name    string
		ternary Ternary
		want    bool
	}{
		{"Unspecified becomes false", Unspecified, false},
		{"True becomes true", True, true},
		{"False becomes false", False, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ternary.Bool(); got != tt.want {
				t.Errorf("Ternary.Bool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKind_Values(t *testing.T) {
	kinds := []Kind{
		Unknown,
		NotFound,
		AlreadyExists,
		Invalid,
		PermissionDenied,
		Unavailable,
		Internal,
		Canceled,
		Conflict,
		Timeout,
		RateLimited,
	}

	seen := make(map[Kind]bool)
	for _, k := range kinds {
		if seen[k] {
			t.Errorf("Duplicate Kind value: %s", k)
		}
		seen[k] = true
	}

	if len(seen) != 11 {
		t.Errorf("Expected 11 unique Kind values, got %d", len(seen))
	}
}

func TestTernary_Values(t *testing.T) {
	ternaries := []Ternary{Unspecified, True, False}
	seen := make(map[Ternary]bool)
	for _, ternary := range ternaries {
		if seen[ternary] {
			t.Errorf("Duplicate Ternary value: %d", ternary)
		}
		seen[ternary] = true
	}

	if len(seen) != 3 {
		t.Errorf("Expected 3 unique Ternary values, got %d", len(seen))
	}
}
