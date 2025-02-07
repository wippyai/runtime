package graph

import (
	"fmt"
)

// Infinity represents an infinite cost in the graph
const Infinity = -1

// DependencyLevels represents nodes grouped into levels based on their dependencies.
// Each level contains nodes that only depend on nodes in previous levels,
// allowing for topological organization of the graph's nodes.
type DependencyLevels[T comparable] struct {
	levels [][]T
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
