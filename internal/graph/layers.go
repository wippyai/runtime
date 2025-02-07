package graph

import (
	"fmt"
	"sort"
)

// Infinity represents an infinite cost in the graph
const Infinity = -1

// DependencyLevels represents nodes grouped into levels based on their dependencies.
// Each level contains nodes that only depend on nodes in previous levels,
// allowing for topological organization of the graph's nodes.
type DependencyLevels[T comparable] struct {
	levels [][]T
}

// DependencyLevels returns the dependency levels of the graph organized in topological order.
// Each level contains nodes that only depend on nodes in previous levels.
// Returns an error if the graph contains a cycle, as cyclic dependencies cannot
// be organized into levels.
func (g *Graph[T]) DependencyLevels() (*DependencyLevels[T], error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Calculate in-degree for each node
	inDegree := make(map[T]int)
	for node := range g.nodes {
		inDegree[node] = 0
	}
	for _, edges := range g.edges {
		for to := range edges {
			inDegree[to]++
		}
	}

	result := &DependencyLevels[T]{
		levels: make([][]T, 0),
	}

	// Continue until all nodes are processed
	foundNodes := true
	for len(inDegree) > 0 && foundNodes {
		// Find all nodes with no dependencies (in-degree = 0)
		currentLevel := make([]T, 0)
		foundNodes = false

		for node, degree := range inDegree {
			if degree == 0 {
				currentLevel = append(currentLevel, node)
				foundNodes = true
			}
		}

		// If we have nodes but none with in-degree 0, we have a cycle
		if !foundNodes && len(inDegree) > 0 {
			remaining := make([]T, 0, len(inDegree))
			for node := range inDegree {
				remaining = append(remaining, node)
			}
			// Sort for stable error message
			sort.Slice(remaining, func(i, j int) bool {
				return fmt.Sprintf("%v", remaining[i]) < fmt.Sprintf("%v", remaining[j])
			})
			return nil, fmt.Errorf("cycle detected with nodes: %v", remaining)
		}

		// Remove current level nodes from consideration
		for _, node := range currentLevel {
			if edges, exists := g.edges[node]; exists {
				for neighbor := range edges {
					inDegree[neighbor]--
				}
			}
			delete(inDegree, node)
		}

		// Sort current level for consistent output
		sort.Slice(currentLevel, func(i, j int) bool {
			return fmt.Sprintf("%v", currentLevel[i]) < fmt.Sprintf("%v", currentLevel[j])
		})

		if len(currentLevel) > 0 {
			result.levels = append(result.levels, currentLevel)
		}
	}

	return result, nil
}

// GetLevel returns a slice containing all nodes at the specified level.
// Returns an error if the level is out of range.
// Level numbering starts at 0.
func (d *DependencyLevels[T]) GetLevel(level int) ([]T, error) {
	if level < 0 || level >= len(d.levels) {
		return nil, fmt.Errorf("invalid level: %d", level)
	}
	return d.levels[level], nil
}

// LevelCount returns the total number of dependency levels in the graph.
func (d *DependencyLevels[T]) LevelCount() int {
	return len(d.levels)
}

// GetNodeLevel returns the level number for a given node.
// Returns -1 if the node is not found in any level.
// Level numbering starts at 0.
func (d *DependencyLevels[T]) GetNodeLevel(node T) int {
	for level, nodes := range d.levels {
		for _, n := range nodes {
			if n == node {
				return level
			}
		}
	}
	return -1
}

// AllLevels returns all dependency levels as a slice of slices.
// Each inner slice contains the nodes at that level, with level
// numbering starting at 0.
func (d *DependencyLevels[T]) AllLevels() [][]T {
	return d.levels
}
