package graph

import (
	"fmt"
	"sort"
)

// DependencyLevels represents nodes grouped by their dependency relationships,
// where each level contains nodes that only depend on nodes in previous levels.
type DependencyLevels struct {
	Levels [][]Node // Each slice contains nodes at the same dependency level
}

// DependencyLevels performs a topological sort of the graph and groups nodes by dependency levels.
//
// Pre-conditions:
//   - The graph is a directed acyclic graph (DAG).
//
// Post-conditions:
//   - Returns a `DependencyLevels` struct where `Levels` contains a slice of nodes for each dependency level.
//   - The nodes within each level are sorted lexicographically.
//   - Returns an error if the graph contains a cycle.
func (g *Graph) DependencyLevels() (*DependencyLevels, error) {
	g.mutex.RLock()
	defer g.mutex.RUnlock()

	// Create a map to store in-degree (number of incoming edges) for each node
	inDegree := make(map[Node]int)

	// Initialize in-degree counts
	for node := range g.nodes {
		inDegree[node] = 0
	}

	// Calculate in-degree for each node
	for _, edges := range g.edges {
		for to := range edges {
			inDegree[to]++
		}
	}

	// Initialize result structure
	result := &DependencyLevels{
		Levels: make([][]Node, 0),
	}

	// Track if any nodes with in-degree 0 were found in an iteration
	foundZeroDegreeNode := true

	// Continue processing until all nodes are assigned to levels or no more nodes with in-degree 0 are found
	for len(inDegree) > 0 && foundZeroDegreeNode {
		// Find all nodes with no incoming edges (in-degree = 0)
		currentLevel := make([]Node, 0)
		foundZeroDegreeNode = false // Reset flag for the current iteration

		for node, degree := range inDegree {
			if degree == 0 {
				currentLevel = append(currentLevel, node)
				foundZeroDegreeNode = true // Found at least one node with in-degree 0
			}
		}

		// If no nodes with in-degree 0 are found, but we still have nodes, we have a cycle
		if !foundZeroDegreeNode && len(inDegree) > 0 {
			return nil, fmt.Errorf("cycle detected in graph, cannot create dependency levels")
		}

		// Remove the nodes in the current level from consideration
		for _, node := range currentLevel {
			// Decrease in-degree for all neighbors
			if edges, exists := g.edges[node]; exists {
				for neighbor := range edges {
					inDegree[neighbor]--
				}
			}
			// Remove the node from in-degree map
			delete(inDegree, node)
		}

		// Add the current level to the result (if it's not empty)
		if len(currentLevel) > 0 {
			// Sort nodes within the level for stable test output and better readability
			sort.Slice(currentLevel, func(i, j int) bool {
				return string(currentLevel[i]) < string(currentLevel[j])
			})
			result.Levels = append(result.Levels, currentLevel)
		}
	}

	return result, nil
}

// GetLevel returns all nodes in a specific dependency level.
//
// Pre-conditions:
//   - `level` is a valid level within the `DependencyLevels` struct (0 <= level < LevelCount()).
//
// Post-conditions:
//   - Returns a slice of nodes at the specified `level`.
//   - Returns an error if the `level` is invalid.
func (dl *DependencyLevels) GetLevel(level int) ([]Node, error) {
	if level < 0 || level >= len(dl.Levels) {
		return nil, fmt.Errorf("dependency level %d does not exist", level)
	}
	return dl.Levels[level], nil
}

// LevelCount returns the total number of dependency levels.
//
// Pre-conditions:
//   - None
//
// Post-conditions:
//   - Returns the number of dependency levels.
func (dl *DependencyLevels) LevelCount() int {
	return len(dl.Levels)
}

// GetNodeLevel returns the dependency level number (0-based) for a given node.
//
// Pre-conditions:
//   - None
//
// Post-conditions:
//   - Returns the dependency level of the `node` if found.
//   - Returns -1 if the `node` is not found in any level.
func (dl *DependencyLevels) GetNodeLevel(node Node) int {
	for i, level := range dl.Levels {
		for _, n := range level {
			if n == node {
				return i
			}
		}
	}
	return -1
}
