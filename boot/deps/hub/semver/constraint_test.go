package semver

import (
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Version
		wantErr bool
	}{
		// Valid versions
		{"simple", "1.0.0", Version{Major: 1, Minor: 0, Patch: 0}, false},
		{"with minor", "1.2.0", Version{Major: 1, Minor: 2, Patch: 0}, false},
		{"with patch", "1.2.3", Version{Major: 1, Minor: 2, Patch: 3}, false},
		{"zero major", "0.1.0", Version{Major: 0, Minor: 1, Patch: 0}, false},
		{"large numbers", "10.20.30", Version{Major: 10, Minor: 20, Patch: 30}, false},
		{"prerelease alpha", "1.0.0-alpha", Version{Major: 1, Minor: 0, Patch: 0, Prerelease: "alpha"}, false},
		{"prerelease beta.1", "1.0.0-beta.1", Version{Major: 1, Minor: 0, Patch: 0, Prerelease: "beta.1"}, false},
		{"prerelease rc.1", "2.0.0-rc.1", Version{Major: 2, Minor: 0, Patch: 0, Prerelease: "rc.1"}, false},
		{"with build", "1.0.0+build", Version{Major: 1, Minor: 0, Patch: 0, Build: "build"}, false},
		{"prerelease and build", "1.0.0-alpha+build", Version{Major: 1, Minor: 0, Patch: 0, Prerelease: "alpha", Build: "build"}, false},

		// Invalid versions
		{"empty", "", Version{}, true},
		{"no patch", "1.0", Version{}, true},
		{"no minor", "1", Version{}, true},
		{"leading v", "v1.0.0", Version{Major: 1, Minor: 0, Patch: 0}, false},
		{"negative", "-1.0.0", Version{}, true},
		{"letters", "a.b.c", Version{}, true},
		{"trailing dot", "1.0.0.", Version{}, true},
		{"leading zero major", "01.0.0", Version{}, true},
		{"leading zero minor", "1.01.0", Version{}, true},
		{"leading zero patch", "1.0.01", Version{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVersion(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseVersion(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseVersion(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		// Equal
		{"equal simple", "1.0.0", "1.0.0", 0},
		{"equal with prerelease", "1.0.0-alpha", "1.0.0-alpha", 0},

		// Major comparison
		{"major less", "1.0.0", "2.0.0", -1},
		{"major greater", "2.0.0", "1.0.0", 1},

		// Minor comparison
		{"minor less", "1.1.0", "1.2.0", -1},
		{"minor greater", "1.2.0", "1.1.0", 1},

		// Patch comparison
		{"patch less", "1.0.1", "1.0.2", -1},
		{"patch greater", "1.0.2", "1.0.1", 1},

		// Prerelease vs release
		{"prerelease less than release", "1.0.0-alpha", "1.0.0", -1},
		{"release greater than prerelease", "1.0.0", "1.0.0-alpha", 1},

		// Prerelease ordering
		{"alpha less than beta", "1.0.0-alpha", "1.0.0-beta", -1},
		{"alpha.1 less than alpha.2", "1.0.0-alpha.1", "1.0.0-alpha.2", -1},
		{"numeric prerelease", "1.0.0-1", "1.0.0-2", -1},
		{"numeric less than alpha", "1.0.0-1", "1.0.0-alpha", -1},
		{"rc.1 less than rc.2", "1.0.0-rc.1", "1.0.0-rc.2", -1},

		// Build metadata ignored
		{"build metadata ignored", "1.0.0+build1", "1.0.0+build2", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _ := ParseVersion(tt.a)
			b, _ := ParseVersion(tt.b)
			got := a.Compare(b)
			if got != tt.want {
				t.Errorf("Compare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestParseConstraint(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Exact version
		{"exact", "1.0.0", false},
		{"exact with equals", "=1.0.0", false},

		// Comparison operators
		{"greater than", ">1.0.0", false},
		{"greater or equal", ">=1.0.0", false},
		{"less than", "<1.0.0", false},
		{"less or equal", "<=1.0.0", false},
		{"not equal", "!=1.0.0", false},

		// Caret (compatible)
		{"caret", "^1.0.0", false},
		{"caret zero major", "^0.1.0", false},
		{"caret zero minor", "^0.0.1", false},

		// Tilde (approximately)
		{"tilde", "~1.0.0", false},
		{"tilde minor", "~1.2.0", false},

		// Range
		{"range space", ">=1.0.0 <2.0.0", false},
		{"range comma", ">=1.0.0,<2.0.0", false},

		// Wildcards
		{"wildcard minor", "1.*", false},
		{"wildcard patch", "1.2.*", false},
		{"wildcard x minor", "1.x", false},
		{"wildcard x patch", "1.2.x", false},

		// With prerelease
		{"prerelease constraint", ">=1.0.0-alpha", false},

		// Invalid
		{"empty", "", true},
		{"invalid version", ">=abc", true},
		{"invalid operator", ">>1.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConstraint(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConstraint(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestConstraintMatch(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		version    string
		want       bool
	}{
		// Exact match
		{"exact match", "1.0.0", "1.0.0", true},
		{"exact no match", "1.0.0", "1.0.1", false},
		{"exact with equals", "=1.0.0", "1.0.0", true},

		// Greater than
		{"gt match", ">1.0.0", "1.0.1", true},
		{"gt no match equal", ">1.0.0", "1.0.0", false},
		{"gt no match less", ">1.0.0", "0.9.9", false},

		// Greater or equal
		{"gte match greater", ">=1.0.0", "1.0.1", true},
		{"gte match equal", ">=1.0.0", "1.0.0", true},
		{"gte no match", ">=1.0.0", "0.9.9", false},

		// Less than
		{"lt match", "<1.0.0", "0.9.9", true},
		{"lt no match equal", "<1.0.0", "1.0.0", false},
		{"lt no match greater", "<1.0.0", "1.0.1", false},

		// Less or equal
		{"lte match less", "<=1.0.0", "0.9.9", true},
		{"lte match equal", "<=1.0.0", "1.0.0", true},
		{"lte no match", "<=1.0.0", "1.0.1", false},

		// Not equal
		{"neq match", "!=1.0.0", "1.0.1", true},
		{"neq no match", "!=1.0.0", "1.0.0", false},

		// Caret (^) - compatible with version
		// ^1.2.3 := >=1.2.3 <2.0.0
		{"caret 1.x lower bound", "^1.2.3", "1.2.3", true},
		{"caret 1.x within", "^1.2.3", "1.9.9", true},
		{"caret 1.x upper bound", "^1.2.3", "2.0.0", false},
		{"caret 1.x below", "^1.2.3", "1.2.2", false},

		// ^0.2.3 := >=0.2.3 <0.3.0
		{"caret 0.x lower bound", "^0.2.3", "0.2.3", true},
		{"caret 0.x within", "^0.2.3", "0.2.9", true},
		{"caret 0.x upper bound", "^0.2.3", "0.3.0", false},

		// ^0.0.3 := >=0.0.3 <0.0.4
		{"caret 0.0.x exact", "^0.0.3", "0.0.3", true},
		{"caret 0.0.x above", "^0.0.3", "0.0.4", false},

		// Tilde (~) - approximately equivalent
		// ~1.2.3 := >=1.2.3 <1.3.0
		{"tilde lower bound", "~1.2.3", "1.2.3", true},
		{"tilde within", "~1.2.3", "1.2.9", true},
		{"tilde upper bound", "~1.2.3", "1.3.0", false},
		{"tilde below", "~1.2.3", "1.2.2", false},

		// ~1.2 := >=1.2.0 <1.3.0
		{"tilde minor", "~1.2.0", "1.2.5", true},
		{"tilde minor upper", "~1.2.0", "1.3.0", false},

		// Range constraints
		{"range match", ">=1.0.0 <2.0.0", "1.5.0", true},
		{"range lower bound", ">=1.0.0 <2.0.0", "1.0.0", true},
		{"range upper bound", ">=1.0.0 <2.0.0", "2.0.0", false},
		{"range below", ">=1.0.0 <2.0.0", "0.9.9", false},
		{"range comma", ">=1.0.0,<2.0.0", "1.5.0", true},

		// Wildcards
		{"wildcard 1.* match", "1.*", "1.5.0", true},
		{"wildcard 1.* no match", "1.*", "2.0.0", false},
		{"wildcard 1.2.* match", "1.2.*", "1.2.5", true},
		{"wildcard 1.2.* no match minor", "1.2.*", "1.3.0", false},
		{"wildcard x notation", "1.x", "1.5.0", true},
		{"wildcard X notation", "1.X", "1.5.0", true},

		// Prerelease handling
		{"prerelease constraint match", ">=1.0.0-alpha", "1.0.0-alpha", true},
		{"prerelease constraint match release", ">=1.0.0-alpha", "1.0.0", true},
		{"prerelease constraint match beta", ">=1.0.0-alpha", "1.0.0-beta", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := ParseConstraint(tt.constraint)
			if err != nil {
				t.Fatalf("ParseConstraint(%q) error = %v", tt.constraint, err)
			}
			v, err := ParseVersion(tt.version)
			if err != nil {
				t.Fatalf("ParseVersion(%q) error = %v", tt.version, err)
			}
			got := c.Match(v)
			if got != tt.want {
				t.Errorf("Constraint(%q).Match(%q) = %v, want %v", tt.constraint, tt.version, got, tt.want)
			}
		})
	}
}

func TestFindBestMatch(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		want       string
		versions   []string
		wantErr    bool
	}{
		{
			name:       "exact match",
			constraint: "1.0.0",
			versions:   []string{"0.9.0", "1.0.0", "1.1.0"},
			want:       "1.0.0",
		},
		{
			name:       "gte picks highest",
			constraint: ">=1.0.0",
			versions:   []string{"0.9.0", "1.0.0", "1.1.0", "2.0.0"},
			want:       "2.0.0",
		},
		{
			name:       "caret picks highest compatible",
			constraint: "^1.0.0",
			versions:   []string{"0.9.0", "1.0.0", "1.5.0", "1.9.9", "2.0.0"},
			want:       "1.9.9",
		},
		{
			name:       "tilde picks highest approximate",
			constraint: "~1.2.0",
			versions:   []string{"1.1.0", "1.2.0", "1.2.5", "1.3.0"},
			want:       "1.2.5",
		},
		{
			name:       "range picks highest in range",
			constraint: ">=1.0.0 <2.0.0",
			versions:   []string{"0.9.0", "1.0.0", "1.5.0", "2.0.0", "2.1.0"},
			want:       "1.5.0",
		},
		{
			name:       "no match returns error",
			constraint: ">=3.0.0",
			versions:   []string{"1.0.0", "2.0.0"},
			wantErr:    true,
		},
		{
			name:       "empty versions returns error",
			constraint: ">=1.0.0",
			versions:   []string{},
			wantErr:    true,
		},
		{
			name:       "prefers stable over prerelease",
			constraint: ">=1.0.0",
			versions:   []string{"1.0.0", "1.1.0-alpha", "1.1.0"},
			want:       "1.1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := ParseConstraint(tt.constraint)
			if err != nil {
				t.Fatalf("ParseConstraint(%q) error = %v", tt.constraint, err)
			}

			versions := make([]Version, 0, len(tt.versions))
			for _, vs := range tt.versions {
				v, err := ParseVersion(vs)
				if err != nil {
					t.Fatalf("ParseVersion(%q) error = %v", vs, err)
				}
				versions = append(versions, v)
			}

			got, err := c.FindBestMatch(versions)
			if (err != nil) != tt.wantErr {
				t.Errorf("FindBestMatch() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got.String() != tt.want {
				t.Errorf("FindBestMatch() = %v, want %v", got.String(), tt.want)
			}
		})
	}
}

func TestValidateConstraint(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid exact", "1.0.0", false},
		{"valid gte", ">=1.0.0", false},
		{"valid caret", "^1.0.0", false},
		{"valid tilde", "~1.0.0", false},
		{"valid range", ">=1.0.0 <2.0.0", false},
		{"valid wildcard", "1.*", false},
		{"invalid empty", "", true},
		{"invalid garbage", "not-a-version", true},
		{"invalid operator", ">>1.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConstraint(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConstraint(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
