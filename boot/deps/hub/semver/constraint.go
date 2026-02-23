// SPDX-License-Identifier: MPL-2.0

package semver

import (
	"errors"
	"regexp"
	"strings"
)

const (
	maxConstraintLength = 256
	maxConstraintParts  = 10
	maxVersionNumber    = 999999
)

var (
	ErrInvalidConstraint = errors.New("invalid semver constraint")
	ErrNoMatchingVersion = errors.New("no matching version found")
	errNumberTooLarge    = errors.New("number too large")
	errNotANumber        = errors.New("not a number")

	// Operators: >=, <=, >, <, =, !=.
	operatorRegex = regexp.MustCompile(`^(>=|<=|>|<|=|!=|\^|~)?(.+)$`)
	// Wildcard patterns: 1.*, 1.x, 1.X, 1.2.*, 1.2.x.
	wildcardRegex = regexp.MustCompile(`^(\d+)\.(\*|[xX]|\d+\.(\*|[xX]))$`)
)

type operator int

const (
	opEQ operator = iota
	opNE
	opGT
	opGTE
	opLT
	opLTE
	opCaret
	opTilde
)

// Constraint represents a version constraint.
type Constraint struct {
	parts []constraintPart
}

type constraintPart struct {
	wildcardType string
	version      Version
	major        int
	minor        int
	op           operator
	wildcard     bool
}

// ParseConstraint parses a version constraint string.
func ParseConstraint(s string) (Constraint, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > maxConstraintLength {
		return Constraint{}, ErrInvalidConstraint
	}

	parts := splitConstraint(s)
	if len(parts) == 0 || len(parts) > maxConstraintParts {
		return Constraint{}, ErrInvalidConstraint
	}

	result := Constraint{parts: make([]constraintPart, 0, len(parts))}

	for _, part := range parts {
		cp, err := parseConstraintPart(part)
		if err != nil {
			return Constraint{}, err
		}
		result.parts = append(result.parts, cp)
	}

	return result, nil
}

func splitConstraint(s string) []string {
	// Replace commas with spaces
	s = strings.ReplaceAll(s, ",", " ")
	// Split by whitespace
	parts := strings.Fields(s)
	return parts
}

func parseConstraintPart(s string) (constraintPart, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return constraintPart{}, ErrInvalidConstraint
	}

	// Standalone * means any version
	if s == "*" {
		return constraintPart{wildcard: true, wildcardType: "any"}, nil
	}

	if wildcardRegex.MatchString(s) {
		return parseWildcard(s)
	}

	matches := operatorRegex.FindStringSubmatch(s)
	if matches == nil {
		return constraintPart{}, ErrInvalidConstraint
	}

	op, ok := parseOperator(matches[1])
	if !ok {
		return constraintPart{}, ErrInvalidConstraint
	}

	v, err := ParseVersion(matches[2])
	if err != nil {
		return constraintPart{}, ErrInvalidConstraint
	}

	return constraintPart{op: op, version: v}, nil
}

func parseOperator(s string) (operator, bool) {
	switch s {
	case "", "=":
		return opEQ, true
	case "!=":
		return opNE, true
	case ">":
		return opGT, true
	case ">=":
		return opGTE, true
	case "<":
		return opLT, true
	case "<=":
		return opLTE, true
	case "^":
		return opCaret, true
	case "~":
		return opTilde, true
	default:
		return 0, false
	}
}

func parseWildcard(s string) (constraintPart, error) {
	parts := strings.Split(s, ".")

	cp := constraintPart{wildcard: true}

	// Parse major
	major, err := parseInt(parts[0])
	if err != nil {
		return constraintPart{}, ErrInvalidConstraint
	}
	cp.major = major

	if len(parts) == 2 {
		// 1.* or 1.x format
		if isWildcard(parts[1]) {
			cp.wildcardType = "minor"
			return cp, nil
		}
		return constraintPart{}, ErrInvalidConstraint
	}

	if len(parts) == 3 {
		// 1.2.* or 1.2.x format
		minor, err := parseInt(parts[1])
		if err != nil {
			return constraintPart{}, ErrInvalidConstraint
		}
		cp.minor = minor
		if isWildcard(parts[2]) {
			cp.wildcardType = "patch"
			return cp, nil
		}
	}

	return constraintPart{}, ErrInvalidConstraint
}

func isWildcard(s string) bool {
	return s == "*" || s == "x" || s == "X"
}

func parseInt(s string) (int, error) {
	if len(s) > 6 {
		return 0, errNumberTooLarge
	}
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errNotANumber
		}
		n = n*10 + int(c-'0')
		if n > maxVersionNumber {
			return 0, errNumberTooLarge
		}
	}
	return n, nil
}

// Match returns true if the version satisfies the constraint.
func (c Constraint) Match(v Version) bool {
	for _, part := range c.parts {
		if !part.match(v) {
			return false
		}
	}
	return true
}

func (cp constraintPart) match(v Version) bool {
	if cp.wildcard {
		return cp.matchWildcard(v)
	}

	switch cp.op {
	case opEQ:
		return v.Equal(cp.version)
	case opNE:
		return !v.Equal(cp.version)
	case opGT:
		return v.GreaterThan(cp.version)
	case opGTE:
		return v.GreaterOrEqual(cp.version)
	case opLT:
		return v.LessThan(cp.version)
	case opLTE:
		return v.LessOrEqual(cp.version)
	case opCaret:
		return cp.matchCaret(v)
	case opTilde:
		return cp.matchTilde(v)
	}
	return false
}

func (cp constraintPart) matchWildcard(v Version) bool {
	switch cp.wildcardType {
	case "any":
		// * matches any version
		return true
	case "minor":
		// 1.* matches any 1.x.x
		return v.Major == cp.major
	case "patch":
		// 1.2.* matches any 1.2.x
		return v.Major == cp.major && v.Minor == cp.minor
	}
	return false
}

// matchCaret implements ^version matching (^1.2.3 := >=1.2.3 <2.0.0).
func (cp constraintPart) matchCaret(v Version) bool {
	base := cp.version

	// Must be >= base version
	if v.LessThan(base) {
		return false
	}

	// Upper bound depends on which is the leftmost non-zero
	if base.Major != 0 {
		// ^1.2.3 -> <2.0.0
		return v.Major == base.Major
	}
	if base.Minor != 0 {
		// ^0.2.3 -> <0.3.0
		return v.Major == 0 && v.Minor == base.Minor
	}
	// ^0.0.3 -> <0.0.4
	return v.Major == 0 && v.Minor == 0 && v.Patch == base.Patch
}

// matchTilde implements ~version matching (~1.2.3 := >=1.2.3 <1.3.0).
func (cp constraintPart) matchTilde(v Version) bool {
	base := cp.version

	// Must be >= base version
	if v.LessThan(base) {
		return false
	}

	// Must be same major and minor
	return v.Major == base.Major && v.Minor == base.Minor
}

// FindBestMatch finds the highest version that satisfies the constraint.
// Prefers stable versions over prereleases.
func (c Constraint) FindBestMatch(versions []Version) (Version, error) {
	if len(versions) == 0 {
		return Version{}, ErrNoMatchingVersion
	}

	best, bestStable := c.findMatches(versions)

	if bestStable != nil {
		return *bestStable, nil
	}
	if best != nil {
		return *best, nil
	}
	return Version{}, ErrNoMatchingVersion
}

func (c Constraint) findMatches(versions []Version) (best, bestStable *Version) {
	for i := range versions {
		v := &versions[i]
		if !c.Match(*v) {
			continue
		}
		if best == nil || v.GreaterThan(*best) {
			best = v
		}
		if !v.IsPrerelease() && (bestStable == nil || v.GreaterThan(*bestStable)) {
			bestStable = v
		}
	}
	return best, bestStable
}

// ValidateConstraint checks if a constraint string is valid.
func ValidateConstraint(s string) error {
	_, err := ParseConstraint(s)
	return err
}

// IsConstraint returns true if the string looks like a constraint (has operators).
// Returns false if it's a plain version string.
func IsConstraint(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// Check for operators or wildcards
	if strings.ContainsAny(s, "><=!^~") {
		return true
	}
	if strings.Contains(s, "*") || strings.HasSuffix(s, ".x") || strings.HasSuffix(s, ".X") {
		return true
	}
	// Check for range (space-separated)
	if strings.Contains(s, " ") || strings.Contains(s, ",") {
		return true
	}
	return false
}
