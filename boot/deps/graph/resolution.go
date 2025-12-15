package graph

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
)

// resolveVersion finds the highest version from labels that matches the constraint.
func resolveVersion(constraint string, labels []*modulev1.Label) (*modulev1.Label, error) {
	if len(labels) == 0 {
		return nil, ErrNoLabelsAvailable
	}

	c, err := parseConstraint(constraint)
	if err != nil {
		return nil, NewParseConstraintError(constraint, err)
	}

	var best *modulev1.Label
	var bestVersion *semver.Version

	for _, label := range labels {
		v, err := semver.NewVersion(label.GetName())
		if err != nil {
			continue
		}

		if c.Check(v) {
			if bestVersion == nil || v.GreaterThan(bestVersion) {
				best = label
				bestVersion = v
			}
		}
	}

	if best == nil {
		return nil, NewNoMatchingVersionError(constraint)
	}

	return best, nil
}

// parseConstraint parses a version constraint string.
func parseConstraint(constraint string) (*semver.Constraints, error) {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || constraint == "*" {
		constraint = "*"
	}

	// Handle implicit exact match (e.g., "1.2.3" → "=1.2.3")
	if !strings.ContainsAny(constraint, ">=<~^*") {
		if _, err := semver.NewVersion(constraint); err == nil {
			constraint = "=" + constraint
		}
	}

	return semver.NewConstraint(constraint)
}

// checkConstraintCompatibility checks if two constraints can be satisfied by a single version.
func checkConstraintCompatibility(c1, c2 *semver.Constraints) bool {
	// Generate test versions to check intersection
	// This is a heuristic - proper intersection would require constraint algebra
	testVersions := generateTestVersions()

	foundC1 := false
	foundC2 := false
	foundBoth := false

	for _, v := range testVersions {
		matchesC1 := c1.Check(v)
		matchesC2 := c2.Check(v)

		if matchesC1 {
			foundC1 = true
		}
		if matchesC2 {
			foundC2 = true
		}
		if matchesC1 && matchesC2 {
			foundBoth = true
			break
		}
	}

	return foundC1 && foundC2 && foundBoth
}

// mergeConstraints attempts to merge multiple constraints into one.
// Returns error if constraints are incompatible.
func mergeConstraints(constraints []string) error {
	if len(constraints) == 0 {
		return ErrNoConstraints
	}

	if len(constraints) == 1 {
		_, err := parseConstraint(constraints[0])
		return err
	}

	// Parse all constraints
	parsed := make([]*semver.Constraints, 0, len(constraints))
	for _, c := range constraints {
		p, err := parseConstraint(c)
		if err != nil {
			return NewParseConstraintError(c, err)
		}
		parsed = append(parsed, p)
	}

	// Check pairwise compatibility
	for i := 0; i < len(parsed); i++ {
		for j := i + 1; j < len(parsed); j++ {
			if !checkConstraintCompatibility(parsed[i], parsed[j]) {
				return NewIncompatibleConstraintsError(constraints[i], constraints[j])
			}
		}
	}

	// Build merged constraint (AND of all constraints)
	merged := strings.Join(constraints, ", ")
	_, err := semver.NewConstraint(merged)
	return err
}

// generateTestVersions generates a set of test versions for compatibility checking.
func generateTestVersions() []*semver.Version {
	versions := make([]*semver.Version, 0, 100)
	for major := 0; major <= 5; major++ {
		for minor := 0; minor <= 10; minor++ {
			for patch := 0; patch <= 10; patch++ {
				v := semver.MustParse(fmt.Sprintf("%d.%d.%d", major, minor, patch))
				versions = append(versions, v)
			}
		}
	}
	return versions
}
