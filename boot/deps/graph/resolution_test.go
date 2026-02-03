package graph

import (
	"testing"

	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
)

func TestParseConstraint(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		// Valid constraints
		{name: "wildcard", input: "*", wantError: false},
		{name: "empty", input: "", wantError: false},
		{name: "exact version", input: "1.2.3", wantError: false},
		{name: "caret", input: "^1.2.3", wantError: false},
		{name: "tilde", input: "~1.2.3", wantError: false},
		{name: "greater than", input: ">1.0.0", wantError: false},
		{name: "greater or equal", input: ">=1.0.0", wantError: false},
		{name: "less than", input: "<2.0.0", wantError: false},
		{name: "less or equal", input: "<=2.0.0", wantError: false},
		{name: "range", input: ">=1.0.0 <2.0.0", wantError: false},
		{name: "with spaces", input: " ^1.2.3 ", wantError: false},
		{name: "x wildcard", input: "1.x", wantError: false},
		{name: "patch wildcard", input: "1.2.x", wantError: false},
		{name: "prerelease", input: "1.0.0-alpha", wantError: false},
		{name: "build metadata", input: "1.0.0+build.123", wantError: false},

		// Invalid constraints
		{name: "invalid format", input: "not-a-version", wantError: true},
		{name: "invalid operator", input: "==1.0.0", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseConstraint(tt.input)
			if tt.wantError && err == nil {
				t.Errorf("parseConstraint(%q) expected error, got nil", tt.input)
			}
			if !tt.wantError && err != nil {
				t.Errorf("parseConstraint(%q) unexpected error: %v", tt.input, err)
			}
		})
	}
}

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		want       string
		labels     []*modulev1.Label
		wantError  bool
	}{
		{
			name:       "exact match",
			constraint: "1.2.3",
			labels: []*modulev1.Label{
				{Name: "1.2.3", CommitId: "abc123"},
			},
			want:      "1.2.3",
			wantError: false,
		},
		{
			name:       "caret picks highest minor",
			constraint: "^1.2.0",
			labels: []*modulev1.Label{
				{Name: "1.2.0", CommitId: "abc"},
				{Name: "1.2.5", CommitId: "def"},
				{Name: "1.3.0", CommitId: "ghi"},
				{Name: "2.0.0", CommitId: "jkl"},
			},
			want:      "1.3.0",
			wantError: false,
		},
		{
			name:       "tilde picks highest patch",
			constraint: "~1.2.0",
			labels: []*modulev1.Label{
				{Name: "1.2.0", CommitId: "abc"},
				{Name: "1.2.5", CommitId: "def"},
				{Name: "1.3.0", CommitId: "ghi"},
			},
			want:      "1.2.5",
			wantError: false,
		},
		{
			name:       "wildcard picks highest",
			constraint: "*",
			labels: []*modulev1.Label{
				{Name: "0.1.0", CommitId: "abc"},
				{Name: "1.0.0", CommitId: "def"},
				{Name: "2.5.3", CommitId: "ghi"},
			},
			want:      "2.5.3",
			wantError: false,
		},
		{
			name:       "range constraint",
			constraint: ">=1.0.0 <2.0.0",
			labels: []*modulev1.Label{
				{Name: "0.9.0", CommitId: "abc"},
				{Name: "1.0.0", CommitId: "def"},
				{Name: "1.5.0", CommitId: "ghi"},
				{Name: "2.0.0", CommitId: "jkl"},
			},
			want:      "1.5.0",
			wantError: false,
		},
		{
			name:       "no matching version",
			constraint: "^3.0.0",
			labels: []*modulev1.Label{
				{Name: "1.0.0", CommitId: "abc"},
				{Name: "2.0.0", CommitId: "def"},
			},
			wantError: true,
		},
		{
			name:       "empty labels",
			constraint: "1.0.0",
			labels:     []*modulev1.Label{},
			wantError:  true,
		},
		{
			name:       "invalid semver in labels",
			constraint: "*",
			labels: []*modulev1.Label{
				{Name: "not-a-version", CommitId: "abc"},
				{Name: "1.0.0", CommitId: "def"},
			},
			want:      "1.0.0",
			wantError: false,
		},
		{
			name:       "prerelease versions",
			constraint: ">=1.0.0-alpha",
			labels: []*modulev1.Label{
				{Name: "1.0.0-alpha", CommitId: "abc"},
				{Name: "1.0.0-beta", CommitId: "def"},
				{Name: "1.0.0", CommitId: "ghi"},
			},
			want:      "1.0.0",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveVersion(tt.constraint, tt.labels)
			if tt.wantError {
				if err == nil {
					t.Errorf("resolveVersion() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("resolveVersion() unexpected error: %v", err)
				return
			}

			if got.GetName() != tt.want {
				t.Errorf("resolveVersion() = %q, want %q", got.GetName(), tt.want)
			}
		})
	}
}

func TestCheckConstraintCompatibility(t *testing.T) {
	tests := []struct {
		name        string
		constraint1 string
		constraint2 string
		compatible  bool
	}{
		{
			name:        "compatible carets same major",
			constraint1: "^1.2.0",
			constraint2: "^1.3.0",
			compatible:  true,
		},
		{
			name:        "incompatible carets different major",
			constraint1: "^1.0.0",
			constraint2: "^2.0.0",
			compatible:  false,
		},
		{
			name:        "compatible tildes same minor",
			constraint1: "~1.2.0",
			constraint2: "~1.2.3",
			compatible:  true,
		},
		{
			name:        "incompatible tildes different minor",
			constraint1: "~1.2.0",
			constraint2: "~1.3.0",
			compatible:  false,
		},
		{
			name:        "compatible ranges with overlap",
			constraint1: ">=1.0.0 <2.0.0",
			constraint2: ">=1.5.0 <3.0.0",
			compatible:  true,
		},
		{
			name:        "incompatible ranges no overlap",
			constraint1: ">=1.0.0 <2.0.0",
			constraint2: ">=2.0.0 <3.0.0",
			compatible:  false,
		},
		{
			name:        "exact same version",
			constraint1: "1.2.3",
			constraint2: "1.2.3",
			compatible:  true,
		},
		{
			name:        "exact different versions",
			constraint1: "1.2.3",
			constraint2: "1.2.4",
			compatible:  false,
		},
		{
			name:        "wildcard with anything",
			constraint1: "*",
			constraint2: "^1.0.0",
			compatible:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c1, err := parseConstraint(tt.constraint1)
			if err != nil {
				t.Fatalf("parseConstraint(%q) error: %v", tt.constraint1, err)
			}

			c2, err := parseConstraint(tt.constraint2)
			if err != nil {
				t.Fatalf("parseConstraint(%q) error: %v", tt.constraint2, err)
			}

			got := checkConstraintCompatibility(c1, c2)
			if got != tt.compatible {
				t.Errorf("checkConstraintCompatibility(%q, %q) = %v, want %v",
					tt.constraint1, tt.constraint2, got, tt.compatible)
			}
		})
	}
}

func TestMergeConstraints(t *testing.T) {
	tests := []struct {
		name        string
		constraints []string
		wantError   bool
	}{
		{
			name:        "single constraint",
			constraints: []string{"^1.0.0"},
			wantError:   false,
		},
		{
			name:        "compatible constraints",
			constraints: []string{">=1.0.0", "<2.0.0"},
			wantError:   false,
		},
		{
			name:        "incompatible constraints",
			constraints: []string{"~1.2.0", "~1.3.0"},
			wantError:   true,
		},
		{
			name:        "multiple compatible",
			constraints: []string{"^1.2.0", ">=1.3.0", "<2.0.0"},
			wantError:   false,
		},
		{
			name:        "empty list",
			constraints: []string{},
			wantError:   true,
		},
		{
			name:        "invalid constraint in list",
			constraints: []string{"^1.0.0", "invalid"},
			wantError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mergeConstraints(tt.constraints)
			if tt.wantError && err == nil {
				t.Errorf("mergeConstraints() expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Errorf("mergeConstraints() unexpected error: %v", err)
			}
		})
	}
}
