// Package error provides error categorization and retry metadata.
package error

import "testing"

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
		ternary Ternary
		want    string
	}{
		{Unspecified, "Unspecified"},
		{True, "True"},
		{False, "False"},
		{Ternary(999), "Unspecified"},
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
