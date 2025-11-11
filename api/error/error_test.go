package error

import "testing"

func TestKind_String(t *testing.T) {
	tests := []struct {
		kind Kind
		want string
	}{
		{KindUnknown, "Unknown"},
		{KindNotFound, "NotFound"},
		{KindAlreadyExists, "AlreadyExists"},
		{KindInvalid, "Invalid"},
		{KindPermissionDenied, "PermissionDenied"},
		{KindUnavailable, "Unavailable"},
		{KindInternal, "Internal"},
		{KindCanceled, "Canceled"},
		{KindConflict, "Conflict"},
		{KindTimeout, "Timeout"},
		{KindRateLimited, "RateLimited"},
		{Kind(999), "Unknown"},
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
		{Unknown, "Unknown"},
		{True, "True"},
		{False, "False"},
		{Ternary(999), "Unknown"},
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
		{"Unknown becomes false", Unknown, false},
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
	if KindUnknown != 0 {
		t.Errorf("KindUnknown should be 0 (iota default), got %d", KindUnknown)
	}

	kinds := []Kind{
		KindUnknown,
		KindNotFound,
		KindAlreadyExists,
		KindInvalid,
		KindPermissionDenied,
		KindUnavailable,
		KindInternal,
		KindCanceled,
		KindConflict,
		KindTimeout,
		KindRateLimited,
	}

	seen := make(map[Kind]bool)
	for _, k := range kinds {
		if seen[k] {
			t.Errorf("Duplicate Kind value: %d", k)
		}
		seen[k] = true
	}

	if len(seen) != 11 {
		t.Errorf("Expected 11 unique Kind values, got %d", len(seen))
	}
}

func TestTernary_Values(t *testing.T) {
	if Unknown != 0 {
		t.Errorf("Unknown should be 0 (iota default), got %d", Unknown)
	}

	ternaries := []Ternary{Unknown, True, False}
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
