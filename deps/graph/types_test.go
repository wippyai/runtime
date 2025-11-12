package graph

import (
	"testing"
)

func TestParseName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      Name
		wantError bool
	}{
		{
			name:  "valid name",
			input: "wippy/actor",
			want: Name{
				Organization: "wippy",
				Module:       "actor",
			},
			wantError: false,
		},
		{
			name:  "valid with numbers",
			input: "org123/module456",
			want: Name{
				Organization: "org123",
				Module:       "module456",
			},
			wantError: false,
		},
		{
			name:  "valid with hyphens",
			input: "my-org/my-module",
			want: Name{
				Organization: "my-org",
				Module:       "my-module",
			},
			wantError: false,
		},
		{
			name:      "missing organization",
			input:     "module",
			wantError: true,
		},
		{
			name:      "missing module",
			input:     "org/",
			wantError: true,
		},
		{
			name:      "empty organization",
			input:     "/module",
			wantError: true,
		},
		{
			name:      "empty string",
			input:     "",
			wantError: true,
		},
		{
			name:      "only slash",
			input:     "/",
			wantError: true,
		},
		{
			name:      "too many slashes",
			input:     "org/module/extra",
			wantError: true,
		},
		{
			name:      "multiple slashes",
			input:     "org//module",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseName(tt.input)
			if tt.wantError {
				if err == nil {
					t.Errorf("ParseName(%q) expected error, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseName(%q) unexpected error: %v", tt.input, err)
				return
			}

			if got != tt.want {
				t.Errorf("ParseName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestMustParseName(t *testing.T) {
	t.Run("valid name", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("MustParseName panicked on valid input: %v", r)
			}
		}()

		got := MustParseName("wippy/actor")
		want := Name{Organization: "wippy", Module: "actor"}
		if got != want {
			t.Errorf("MustParseName = %v, want %v", got, want)
		}
	})

	t.Run("invalid name panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("MustParseName should panic on invalid input")
			}
		}()

		MustParseName("invalid")
	})
}

func TestNameString(t *testing.T) {
	tests := []struct {
		name Name
		want string
	}{
		{
			name: Name{Organization: "wippy", Module: "actor"},
			want: "wippy/actor",
		},
		{
			name: Name{Organization: "org", Module: "mod"},
			want: "org/mod",
		},
		{
			name: Name{},
			want: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.name.String()
			if got != tt.want {
				t.Errorf("Name.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNameIsZero(t *testing.T) {
	tests := []struct {
		name Name
		want bool
	}{
		{
			name: Name{},
			want: true,
		},
		{
			name: Name{Organization: "", Module: ""},
			want: true,
		},
		{
			name: Name{Organization: "wippy", Module: ""},
			want: false,
		},
		{
			name: Name{Organization: "", Module: "actor"},
			want: false,
		},
		{
			name: Name{Organization: "wippy", Module: "actor"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name.String(), func(t *testing.T) {
			got := tt.name.IsZero()
			if got != tt.want {
				t.Errorf("Name.IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModuleKeyString(t *testing.T) {
	tests := []struct {
		key  ModuleKey
		want string
	}{
		{
			key: ModuleKey{
				Name:    MustParseName("wippy/actor"),
				Version: "1.2.3",
			},
			want: "wippy/actor@1.2.3",
		},
		{
			key: ModuleKey{
				Name:    MustParseName("org/mod"),
				Version: "0.1.0",
			},
			want: "org/mod@0.1.0",
		},
		{
			key: ModuleKey{
				Name:    Name{},
				Version: "",
			},
			want: "/@",
		},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.key.String()
			if got != tt.want {
				t.Errorf("ModuleKey.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConflictReasonString(t *testing.T) {
	tests := []struct {
		reason ConflictReason
		want   string
	}{
		{
			reason: ConflictIncompatibleConstraints,
			want:   "incompatible_constraints",
		},
		{
			reason: ConflictNoMatchingVersion,
			want:   "no_matching_version",
		},
		{
			reason: ConflictCircularDependency,
			want:   "circular_dependency",
		},
		{
			reason: ConflictReason(999),
			want:   "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.reason.String()
			if got != tt.want {
				t.Errorf("ConflictReason.String() = %q, want %q", got, tt.want)
			}
		})
	}
}
