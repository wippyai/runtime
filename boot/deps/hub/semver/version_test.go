// SPDX-License-Identifier: MPL-2.0

package semver

import (
	"testing"
)

func TestVersion_String(t *testing.T) {
	tests := []struct {
		want    string
		version Version
	}{
		{want: "1.0.0", version: Version{Major: 1, Minor: 0, Patch: 0}},
		{want: "2.3.4", version: Version{Major: 2, Minor: 3, Patch: 4}},
		{want: "1.0.0-alpha", version: Version{Major: 1, Minor: 0, Patch: 0, Prerelease: "alpha"}},
		{want: "1.0.0+build", version: Version{Major: 1, Minor: 0, Patch: 0, Build: "build"}},
		{want: "1.0.0-alpha+build", version: Version{Major: 1, Minor: 0, Patch: 0, Prerelease: "alpha", Build: "build"}},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.version.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersion_LessThan(t *testing.T) {
	v1 := Version{Major: 1, Minor: 0, Patch: 0}
	v2 := Version{Major: 2, Minor: 0, Patch: 0}

	if !v1.LessThan(v2) {
		t.Error("1.0.0 should be less than 2.0.0")
	}
	if v2.LessThan(v1) {
		t.Error("2.0.0 should not be less than 1.0.0")
	}
	if v1.LessThan(v1) {
		t.Error("1.0.0 should not be less than 1.0.0")
	}
}

func TestVersion_LessOrEqual(t *testing.T) {
	v1 := Version{Major: 1, Minor: 0, Patch: 0}
	v2 := Version{Major: 2, Minor: 0, Patch: 0}

	if !v1.LessOrEqual(v2) {
		t.Error("1.0.0 should be less or equal to 2.0.0")
	}
	if !v1.LessOrEqual(v1) {
		t.Error("1.0.0 should be less or equal to 1.0.0")
	}
	if v2.LessOrEqual(v1) {
		t.Error("2.0.0 should not be less or equal to 1.0.0")
	}
}

func TestVersion_GreaterThan(t *testing.T) {
	v1 := Version{Major: 1, Minor: 0, Patch: 0}
	v2 := Version{Major: 2, Minor: 0, Patch: 0}

	if !v2.GreaterThan(v1) {
		t.Error("2.0.0 should be greater than 1.0.0")
	}
	if v1.GreaterThan(v2) {
		t.Error("1.0.0 should not be greater than 2.0.0")
	}
	if v1.GreaterThan(v1) {
		t.Error("1.0.0 should not be greater than 1.0.0")
	}
}

func TestVersion_GreaterOrEqual(t *testing.T) {
	v1 := Version{Major: 1, Minor: 0, Patch: 0}
	v2 := Version{Major: 2, Minor: 0, Patch: 0}

	if !v2.GreaterOrEqual(v1) {
		t.Error("2.0.0 should be greater or equal to 1.0.0")
	}
	if !v1.GreaterOrEqual(v1) {
		t.Error("1.0.0 should be greater or equal to 1.0.0")
	}
	if v1.GreaterOrEqual(v2) {
		t.Error("1.0.0 should not be greater or equal to 2.0.0")
	}
}

func TestVersion_Equal(t *testing.T) {
	v1 := Version{Major: 1, Minor: 0, Patch: 0}
	v2 := Version{Major: 1, Minor: 0, Patch: 0}
	v3 := Version{Major: 2, Minor: 0, Patch: 0}

	if !v1.Equal(v2) {
		t.Error("1.0.0 should equal 1.0.0")
	}
	if v1.Equal(v3) {
		t.Error("1.0.0 should not equal 2.0.0")
	}

	// Build metadata ignored
	v1b := Version{Major: 1, Minor: 0, Patch: 0, Build: "build1"}
	v2b := Version{Major: 1, Minor: 0, Patch: 0, Build: "build2"}
	if !v1b.Equal(v2b) {
		t.Error("versions with different build metadata should be equal")
	}
}

func TestVersion_IsPrerelease(t *testing.T) {
	release := Version{Major: 1, Minor: 0, Patch: 0}
	prerelease := Version{Major: 1, Minor: 0, Patch: 0, Prerelease: "alpha"}

	if release.IsPrerelease() {
		t.Error("release version should not be prerelease")
	}
	if !prerelease.IsPrerelease() {
		t.Error("prerelease version should be prerelease")
	}
}

func TestCmpInt(t *testing.T) {
	if cmpInt(1, 2) != -1 {
		t.Error("cmpInt(1, 2) should be -1")
	}
	if cmpInt(2, 1) != 1 {
		t.Error("cmpInt(2, 1) should be 1")
	}
	if cmpInt(1, 1) != 0 {
		t.Error("cmpInt(1, 1) should be 0")
	}
}

func TestComparePrereleases(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"alpha", "alpha", 0},
		{"alpha", "beta", -1},
		{"beta", "alpha", 1},
		{"alpha.1", "alpha.2", -1},
		{"alpha.2", "alpha.1", 1},
		{"alpha", "alpha.1", -1},
		{"alpha.1", "alpha", 1},
		{"1", "2", -1},
		{"2", "1", 1},
		{"1", "alpha", -1},
		{"alpha", "1", 1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			if got := comparePrereleases(tt.a, tt.b); got != tt.want {
				t.Errorf("comparePrereleases(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestComparePrereleaseIdentifier(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1", "2", -1},
		{"2", "1", 1},
		{"1", "1", 0},
		{"alpha", "beta", -1},
		{"beta", "alpha", 1},
		{"alpha", "alpha", 0},
		{"1", "alpha", -1},
		{"alpha", "1", 1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			if got := comparePrereleaseIdentifier(tt.a, tt.b); got != tt.want {
				t.Errorf("comparePrereleaseIdentifier(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestErrInvalidVersion(t *testing.T) {
	if ErrInvalidVersion == nil {
		t.Fatal("ErrInvalidVersion should not be nil")
	}
	if ErrInvalidVersion.Error() == "" {
		t.Error("ErrInvalidVersion should have a message")
	}
}
