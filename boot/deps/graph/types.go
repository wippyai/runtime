package graph

import (
	"strings"

	identityv1 "github.com/wippyai/module-registry-proto-go/registry/identity/v1"
	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
	"github.com/wippyai/runtime/internal/graph"
)

// Name represents a module name in org/module format.
type Name struct {
	Organization string
	Module       string
}

// ParseName parses a module name string into Name.
func ParseName(s string) (Name, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return Name{}, NewInvalidModuleNameError(s)
	}
	if parts[0] == "" || parts[1] == "" {
		return Name{}, NewEmptyModuleNameError(s)
	}
	return Name{
		Organization: parts[0],
		Module:       parts[1],
	}, nil
}

// MustParseName parses a module name or panics.
func MustParseName(s string) Name {
	n, err := ParseName(s)
	if err != nil {
		panic(err)
	}
	return n
}

// String returns the module name as org/module.
func (n Name) String() string {
	return n.Organization + "/" + n.Module
}

// IsZero returns true if the name is empty.
func (n Name) IsZero() bool {
	return n.Organization == "" && n.Module == ""
}

// ModuleKey uniquely identifies a resolved module by name and version.
type ModuleKey struct {
	Name    Name
	Version string
}

// String returns a string representation of the module key.
func (k ModuleKey) String() string {
	return k.Name.String() + "@" + k.Version
}

// DependencyRequest represents a dependency to be resolved.
type DependencyRequest struct {
	Name        Name
	Constraint  string
	RequestedBy string // ID of parent module (empty for root)
}

// BuildInput contains the input for building a dependency graph.
type BuildInput struct {
	RootDependencies []DependencyRequest
}

// BuildResult contains the result of building a dependency graph.
type BuildResult struct {
	// Successfully resolved dependency graph
	Graph *graph.Graph[ModuleKey, DependencyEdge]

	// All module versions that were resolved
	ResolvedModules map[ModuleKey]ResolvedModule

	// Conflicts detected during resolution
	Conflicts []Conflict

	// Processing statistics
	Stats BuildStats
}

// ResolvedModule contains information about a resolved module.
type ResolvedModule struct {
	Organization *identityv1.Organization
	Module       *modulev1.Module
	Name         Name
	Version      string
	CommitID     string
	Constraint   string
	Labels       []*modulev1.Label
	RequestPaths [][]ModuleKey
}

// DependencyEdge represents an edge in the dependency graph.
type DependencyEdge struct {
	Constraint string
	DeclaredBy ModuleKey
	IsDirect   bool // true if this is a root dependency
}

// Conflict represents a dependency conflict.
type Conflict struct {
	Module      Name
	Message     string
	Constraints []ConstraintRequest
	Reason      ConflictReason
}

// ConstraintRequest represents a constraint request from a parent.
type ConstraintRequest struct {
	Constraint  string
	RequestedBy []ModuleKey
}

// ConflictReason indicates the type of conflict.
type ConflictReason int

const (
	ConflictIncompatibleConstraints ConflictReason = iota
	ConflictNoMatchingVersion
	ConflictCircularDependency
)

// String returns a string representation of the conflict reason.
func (r ConflictReason) String() string {
	switch r {
	case ConflictIncompatibleConstraints:
		return "incompatible_constraints"
	case ConflictNoMatchingVersion:
		return "no_matching_version"
	case ConflictCircularDependency:
		return "circular_dependency"
	default:
		return "unknown"
	}
}

// BuildStats contains statistics about the build process.
type BuildStats struct {
	TotalModules     int
	TotalLevels      int
	ConflictsFound   int
	ManifestsFetched int
}

// pendingModule represents a module pending resolution.
type pendingModule struct {
	Parent  *ModuleKey
	Request DependencyRequest
	Level   int
}

// constraintSet tracks all constraints for a single module.
type constraintSet struct {
	Module      Name
	Constraints []ConstraintRequest
}
