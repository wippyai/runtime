package graph

import (
	"fmt"
	"sort"
)

// DependencyLevels represents nodes grouped by their dependency relationships,
// where each level contains nodes that only depend on nodes in previous levels
type DependencyLevels struct {
	Levels [][]Node // Each slice contains nodes at the same dependency level
}

// NewDependencyLevels performs a topological sort of the graph and groups nodes by dependency levels
func (g *Graph) NewDependencyLevels() (*DependencyLevels, error) {
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

	// Continue processing until all nodes are assigned to levels
	for len(inDegree) > 0 {
		// Find all nodes with no incoming edges (in-degree = 0)
		currentLevel := make([]Node, 0)
		for node, degree := range inDegree {
			if degree == 0 {
				currentLevel = append(currentLevel, node)
			}
		}

		// If no nodes with in-degree 0 found but we still have nodes, we have a cycle
		if len(currentLevel) == 0 && len(inDegree) > 0 {
			return nil, fmt.Errorf("cycle detected in graph, cannot create dependency levels")
		}

		// Remove the nodes in current level from consideration
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

		// Add the current level to result
		if len(currentLevel) > 0 {
			// Sort nodes within the level for stable test output
			sort.Slice(currentLevel, func(i, j int) bool {
				return string(currentLevel[i]) < string(currentLevel[j])
			})
			result.Levels = append(result.Levels, currentLevel)
		}
	}

	return result, nil
}

// GetLevel returns all nodes in a specific dependency level
func (dl *DependencyLevels) GetLevel(level int) ([]Node, error) {
	if level < 0 || level >= len(dl.Levels) {
		return nil, fmt.Errorf("dependency level %d does not exist", level)
	}
	return dl.Levels[level], nil
}

// LevelCount returns the total number of dependency levels
func (dl *DependencyLevels) LevelCount() int {
	return len(dl.Levels)
}

// GetNodeLevel returns the dependency level number (0-based) for a given node
// Returns -1 if node is not found
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
