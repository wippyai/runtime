package semver

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrInvalidVersion = errors.New("invalid semver version")

	// Semantic version regex: major.minor.patch with optional prerelease and build metadata.
	versionRegex = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)
)

// Version represents a parsed semantic version.
type Version struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
	Build      string
}

// ParseVersion parses a semantic version string.
func ParseVersion(s string) (Version, error) {
	if s == "" {
		return Version{}, ErrInvalidVersion
	}

	// Strip optional 'v' prefix
	s = strings.TrimPrefix(s, "v")

	matches := versionRegex.FindStringSubmatch(s)
	if matches == nil {
		return Version{}, ErrInvalidVersion
	}

	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])

	return Version{
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		Prerelease: matches[4],
		Build:      matches[5],
	}, nil
}

// String returns the version as a string.
func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Prerelease != "" {
		s += "-" + v.Prerelease
	}
	if v.Build != "" {
		s += "+" + v.Build
	}
	return s
}

// Compare compares two versions.
// Returns -1 if v < other, 0 if v == other, 1 if v > other.
// Build metadata is ignored per semver spec.
func (v Version) Compare(other Version) int {
	if v.Major != other.Major {
		return cmpInt(v.Major, other.Major)
	}
	if v.Minor != other.Minor {
		return cmpInt(v.Minor, other.Minor)
	}
	if v.Patch != other.Patch {
		return cmpInt(v.Patch, other.Patch)
	}

	// Prerelease comparison per semver spec
	if v.Prerelease == "" && other.Prerelease == "" {
		return 0
	}
	if v.Prerelease == "" {
		return 1 // release > prerelease
	}
	if other.Prerelease == "" {
		return -1 // prerelease < release
	}

	return comparePrereleases(v.Prerelease, other.Prerelease)
}

// LessThan returns true if v < other.
func (v Version) LessThan(other Version) bool {
	return v.Compare(other) < 0
}

// LessOrEqual returns true if v <= other.
func (v Version) LessOrEqual(other Version) bool {
	return v.Compare(other) <= 0
}

// GreaterThan returns true if v > other.
func (v Version) GreaterThan(other Version) bool {
	return v.Compare(other) > 0
}

// GreaterOrEqual returns true if v >= other.
func (v Version) GreaterOrEqual(other Version) bool {
	return v.Compare(other) >= 0
}

// Equal returns true if v == other (ignoring build metadata).
func (v Version) Equal(other Version) bool {
	return v.Compare(other) == 0
}

// IsPrerelease returns true if the version has prerelease info.
func (v Version) IsPrerelease() bool {
	return v.Prerelease != ""
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func comparePrereleases(a, b string) int {
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")

	for i := 0; i < len(partsA) && i < len(partsB); i++ {
		cmp := comparePrereleaseIdentifier(partsA[i], partsB[i])
		if cmp != 0 {
			return cmp
		}
	}

	return cmpInt(len(partsA), len(partsB))
}

func comparePrereleaseIdentifier(a, b string) int {
	numA, errA := strconv.Atoi(a)
	numB, errB := strconv.Atoi(b)

	if errA == nil && errB == nil {
		return cmpInt(numA, numB)
	}
	if errA == nil {
		return -1 // numeric < alphanumeric
	}
	if errB == nil {
		return 1 // alphanumeric > numeric
	}

	return strings.Compare(a, b)
}
