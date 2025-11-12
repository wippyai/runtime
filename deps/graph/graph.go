// Package graph provides dependency graph resolution for modules.
//
// The graph builder performs pure dependency resolution by:
//   - Taking root dependencies with version constraints
//   - Recursively fetching manifests via a provider
//   - Building a dependency graph using breadth-first traversal
//   - Detecting conflicts (incompatible constraints, cycles, missing versions)
//   - Returning a complete resolution result
//
// The builder is designed to be pure and testable with no side effects
// beyond calling the provided ManifestProvider interface.
package graph

import (
	"context"
	"fmt"
)

// Builder builds dependency graphs from root dependencies.
type Builder struct {
	provider ManifestProvider
}

// NewBuilder creates a new graph builder.
func NewBuilder(provider ManifestProvider) *Builder {
	return &Builder{
		provider: provider,
	}
}

// Build performs dependency resolution and returns a complete graph.
//
// The algorithm works as follows:
//  1. Start with root dependencies (level 0)
//  2. For each level:
//     - Batch fetch manifests for all pending modules
//     - Resolve versions (highest matching constraint)
//     - Add nodes and edges to graph
//     - Record constraints for overlap detection
//     - Queue transitive dependencies for next level
//  3. After all levels processed:
//     - Detect overlaps (same module, multiple constraints)
//     - Check constraint compatibility
//     - Detect cycles using graph algorithms
//     - Report all conflicts
//  4. Return BuildResult with graph, resolved modules, and conflicts
//
// The function is pure: given the same inputs and provider responses,
// it will produce deterministic output with no side effects.
func (b *Builder) Build(ctx context.Context, input BuildInput) (*BuildResult, error) {
	if b.provider == nil {
		return nil, fmt.Errorf("manifest provider is required")
	}

	if len(input.RootDependencies) == 0 {
		return &BuildResult{
			ResolvedModules: make(map[ModuleKey]ResolvedModule),
			Conflicts:       []Conflict{},
		}, nil
	}

	return b.buildGraph(ctx, input)
}
