package code

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	runtime "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/internal/graph"
)

type (
	// Version tracks changes to nodes
	Version struct {
		Hash    string    // Content hash
		Created time.Time // Creation timestamp
	}

	// Node represents a code unit in the dependency graph.
	// A node may contain either a Lua prototype (Proto) or a module reference.
	Node struct {
		ID      registry.ID
		Kind    registry.Kind
		Version Version
		Source  string         // Code and libs has this
		Method  string         // Processes and functions has this
		Module  runtime.Module // Modules only has this
	}

	Preload struct {
		Name     string
		ModuleID registry.ID
	}

	Dependency struct {
		Name string
		ID   registry.ID
		Node *Node
	}

	Import struct {
		ID    registry.ID
		Alias string
	}

	Edge struct {
		As string
	}

	// Main aggregates a main function prototype, its method,
	// all dependency prototypes, and any required modules.
	Main struct {
		Main         *Node
		Dependencies []Dependency
	}
)

func (p Preload) String() string {
	return fmt.Sprintf("`%s` -> `%s`", p.Name, p.ModuleID)
}

// HashNode computes a hash key based on node.Source and node.Method.
func HashNode(node *Node) string {
	h := sha256.New()
	h.Write([]byte(node.Source))
	h.Write([]byte(node.Method))
	return hex.EncodeToString(h.Sum(nil))
}

// MemoryGraph is an in‑memory implementation of the CodeGraph interface.
// It maintains nodes (representing code units/modules) and their dependency edges.
// Dependency edges (of type runtime.Edge) carry an alias, which is later propagated into
// the final runtime configuration as part of the runtime.Dependency wrapper.
type MemoryGraph struct {
	mu    sync.RWMutex // Protect concurrent access to graph and maps
	graph *graph.Graph[registry.ID, Edge]
	nodes map[registry.ID]*Node

	// Performance caches
	stringCache           map[registry.ID]string  // Cache for ID.String() results
	dependencyLevelsCache [][]*Node               // Cache for DependencyLevels result
	dependentsCache       map[registry.ID][]*Node // Cache for GetAllDependents results
	cacheValid            bool                    // Whether caches are valid
}

// NewMemoryGraph creates a new MemoryGraph instance.
func NewMemoryGraph() *MemoryGraph {
	return &MemoryGraph{
		graph:           graph.New[registry.ID, Edge](),
		nodes:           make(map[registry.ID]*Node),
		stringCache:     make(map[registry.ID]string),
		dependentsCache: make(map[registry.ID][]*Node),
		cacheValid:      true,
	}
}

// invalidateCache marks all caches as invalid
func (m *MemoryGraph) invalidateCache() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cacheValid = false
	// Clear dependents cache since it's most likely to be stale
	for k := range m.dependentsCache {
		delete(m.dependentsCache, k)
	}
}

// invalidateCacheInternal is the internal version that doesn't acquire the lock
// It should only be called from methods that already hold the lock
func (m *MemoryGraph) invalidateCacheInternal() {
	m.cacheValid = false
	// Clear dependents cache since it's most likely to be stale
	for k := range m.dependentsCache {
		delete(m.dependentsCache, k)
	}
}

// getIDString returns a cached string representation of the ID
func (m *MemoryGraph) getIDString(id registry.ID) string {
	m.mu.RLock()
	if str, exists := m.stringCache[id]; exists {
		m.mu.RUnlock()
		return str
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if str, exists := m.stringCache[id]; exists {
		return str
	}

	str := id.String()
	m.stringCache[id] = str
	return str
}

// getIDStringInternal gets the string representation of an ID without acquiring locks
// This method assumes the caller already holds the appropriate lock
func (m *MemoryGraph) getIDStringInternal(id registry.ID) string {
	if str, exists := m.stringCache[id]; exists {
		return str
	}

	str := id.String()
	m.stringCache[id] = str
	return str
}

// hasCycle performs DFS-based cycle detection without cloning the graph
func (m *MemoryGraph) hasCycle(from, to registry.ID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.hasCycleInternal(from, to)
}

// hasCycleInternal performs DFS-based cycle detection without acquiring locks
// This method assumes the caller already holds the appropriate lock
func (m *MemoryGraph) hasCycleInternal(from, to registry.ID) bool {
	if from == to {
		return true
	}

	// Check if there's already a path from 'to' to 'from'
	// If there is, then adding 'from' -> 'to' would create a cycle
	visited := make(map[registry.ID]bool)

	var dfs func(registry.ID) bool
	dfs = func(current registry.ID) bool {
		if current == from {
			return true // Found path from 'to' to 'from'
		}
		if visited[current] {
			return false // Already explored this path
		}

		visited[current] = true

		// Check if we can reach 'from' through any neighbor
		neighbors, err := m.graph.GetNeighbors(current)
		if err != nil {
			return false
		}

		for _, neighbor := range neighbors {
			if dfs(neighbor) {
				return true
			}
		}

		return false
	}

	return dfs(to)
}

// AddNode inserts a new node into the graph.
func (m *MemoryGraph) AddNode(n *Node) error {
	if n == nil {
		return fmt.Errorf("node cannot be nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.nodes[n.ID]; exists {
		return fmt.Errorf("node with Process %v already exists", n.ID)
	}

	m.graph.AddNode(n.ID)
	m.nodes[n.ID] = n
	m.invalidateCacheInternal()
	return nil
}

// RemoveNode deletes a node and its associated edges from the graph.
// It returns an error if the node has any direct outgoing dependencies or incoming dependents.
func (m *MemoryGraph) RemoveNode(id registry.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.nodes[id]; !exists {
		return fmt.Errorf("node with Process %v not found", id)
	}

	// Check for incoming dependencies.
	for nid := range m.nodes {
		if m.graph.HasEdge(nid, id) {
			return fmt.Errorf("cannot remove node %v: it has incoming dependencies from node %v", id, nid)
		}
	}

	if err := m.graph.RemoveNode(id); err != nil {
		return err
	}

	delete(m.nodes, id)
	delete(m.stringCache, id)
	m.invalidateCacheInternal()
	return nil
}

// ReplaceNode atomically replaces an existing node with a new one.
// This method ensures thread-safe node updates without race conditions.
func (m *MemoryGraph) ReplaceNode(newNode *Node) error {
	if newNode == nil {
		return fmt.Errorf("node cannot be nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.nodes[newNode.ID]; !exists {
		return fmt.Errorf("node with Process %v not found", newNode.ID)
	}

	// Atomically replace the node
	m.nodes[newNode.ID] = newNode
	m.invalidateCacheInternal()
	return nil
}

// AddDependency creates a dependency edge from the node with Process 'from' to the node with Process 'to'.
func (m *MemoryGraph) AddDependency(from, to registry.ID, alias string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.nodes[from]; !exists {
		return fmt.Errorf("from node %v not found", from)
	}
	if _, exists := m.nodes[to]; !exists {
		return fmt.Errorf("to node %v not found", to)
	}

	if m.graph.HasEdge(from, to) {
		return fmt.Errorf("dependency from %v to %v already exists", from, to)
	}

	// Check for alias collisions - if this node has other direct dependencies
	// with the same alias but different target nodes
	if alias != "" {
		neighbors, err := m.graph.GetNeighbors(from)
		if err != nil {
			return err
		}
		for _, neighbor := range neighbors {
			if edge, ok := m.graph.GetEdge(from, neighbor); ok {
				if edge.Data.As == alias && neighbor != to {
					return fmt.Errorf("alias collision: %s is already used for another dependency of %v", alias, from)
				}
			}
		}
	}

	// Efficient cycle detection without graph cloning
	if m.hasCycleInternal(from, to) {
		return fmt.Errorf("adding dependency would create a cycle")
	}

	m.graph.AddEdge(from, to, 1, Edge{As: alias})
	m.invalidateCacheInternal()
	return nil
}

// RemoveDependency removes the dependency edge from 'from' to 'to'.
func (m *MemoryGraph) RemoveDependency(from, to registry.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.graph.HasEdge(from, to) {
		return fmt.Errorf("dependency from %v to %v not found", from, to)
	}
	m.invalidateCacheInternal()
	return m.graph.RemoveEdge(from, to)
}

// GetNode retrieves the node with the specified Process.
func (m *MemoryGraph) GetNode(id registry.ID) (*Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	n, exists := m.nodes[id]
	if !exists {
		return nil, fmt.Errorf("node with Process %v not found", id)
	}
	return n, nil
}

// GetDirectDependencies returns all direct dependencies of the specified node.
func (m *MemoryGraph) GetDirectDependencies(id registry.ID) ([]*Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, exists := m.nodes[id]; !exists {
		return nil, fmt.Errorf("node with Process %v not found", id)
	}

	neighbors, err := m.graph.GetNeighbors(id)
	if err != nil {
		return nil, err
	}

	deps := make([]*Node, 0, len(neighbors))
	for _, neighbor := range neighbors {
		if node, exists := m.nodes[neighbor]; exists {
			deps = append(deps, node)
		}
	}

	return deps, nil
}

// GetDirectDependents returns all nodes that directly depend on the specified node.
func (m *MemoryGraph) GetDirectDependents(id registry.ID) ([]*Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, exists := m.nodes[id]; !exists {
		return nil, fmt.Errorf("node with Process %v not found", id)
	}

	dependents := make([]*Node, 0)
	for nid, node := range m.nodes {
		if nid != id {
			if m.graph.HasEdge(nid, id) {
				dependents = append(dependents, node)
			}
		}
	}

	return dependents, nil
}

// GetAllDependents returns all nodes that depend on the specified node (directly or indirectly).
func (m *MemoryGraph) GetAllDependents(id registry.ID) ([]*Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, exists := m.nodes[id]; !exists {
		return nil, fmt.Errorf("node with Process %v not found", id)
	}

	// Check cache first
	if m.cacheValid {
		if cached, exists := m.dependentsCache[id]; exists {
			return cached, nil
		}
	}

	// Use BFS to find all dependents
	visited := make(map[registry.ID]bool)
	addedToResult := make(map[registry.ID]bool) // Track which nodes have been added to result
	queue := []registry.ID{id}
	dependents := make([]*Node, 0)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if visited[current] {
			continue
		}
		visited[current] = true

		// Find all nodes that depend on current
		for nid, node := range m.nodes {
			if nid != current && !visited[nid] {
				if m.graph.HasEdge(nid, current) {
					// Only add to result if not already added
					if !addedToResult[nid] {
						dependents = append(dependents, node)
						addedToResult[nid] = true
					}
					queue = append(queue, nid)
				}
			}
		}
	}

	// Cache the result
	if m.cacheValid {
		m.dependentsCache[id] = dependents
	}

	return dependents, nil
}

// DependencyLevels returns all nodes organized by their dependency levels.
// Level 0 contains nodes with no dependencies, level 1 contains nodes that depend only on level 0 nodes, etc.
func (m *MemoryGraph) DependencyLevels() ([][]*Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.cacheValid && m.dependencyLevelsCache != nil {
		return m.dependencyLevelsCache, nil
	}

	// Calculate in-degrees for all nodes
	inDegree := make(map[registry.ID]int)
	for nid := range m.nodes {
		inDegree[nid] = 0
	}

	// Count incoming edges for each node
	for nid := range m.nodes {
		neighbors, err := m.graph.GetNeighbors(nid)
		if err != nil {
			return nil, err
		}
		for _, neighbor := range neighbors {
			inDegree[neighbor]++
		}
	}

	// Topological sort using Kahn's algorithm
	var levels [][]*Node
	queue := make([]registry.ID, 0)

	// Start with nodes that have no dependencies
	for nid, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, nid)
		}
	}

	level := 0
	for len(queue) > 0 {
		levelSize := len(queue)
		currentLevel := make([]*Node, 0, levelSize)

		for i := 0; i < levelSize; i++ {
			current := queue[i]
			if node, exists := m.nodes[current]; exists {
				currentLevel = append(currentLevel, node)
			}

			// Decrease in-degree for all neighbors
			neighbors, err := m.graph.GetNeighbors(current)
			if err != nil {
				return nil, err
			}
			for _, neighbor := range neighbors {
				inDegree[neighbor]--
				if inDegree[neighbor] == 0 {
					queue = append(queue, neighbor)
				}
			}
		}

		levels = append(levels, currentLevel)
		queue = queue[levelSize:]
		level++
	}

	// Check for cycles (nodes with remaining in-degree)
	for _, degree := range inDegree {
		if degree > 0 {
			return nil, fmt.Errorf("dependency cycle detected")
		}
	}

	// Cache the result
	if m.cacheValid {
		m.dependencyLevelsCache = levels
	}

	return levels, nil
}

// Build creates a Main struct for the given entrypoint, including all its dependencies.
func (m *MemoryGraph) Build(entrypoint registry.ID) (*Main, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, exists := m.nodes[entrypoint]; !exists {
		return nil, fmt.Errorf("entrypoint node %v not found", entrypoint)
	}

	// Get all reachable nodes from entrypoint
	reachable := m.reachableFrom(entrypoint)

	// Build dependency list with proper aliases, including multiple entries for nodes
	// reached through different paths
	dependencies := make([]Dependency, 0)
	seenAliases := make(map[string]bool) // Track unique alias+node combinations

	for id := range reachable {
		if id != entrypoint {
			node, exists := m.nodes[id]
			if !exists {
				continue
			}

			// Find all aliases for this dependency by tracing all dependency paths
			aliases := m.findAllDependencyAliases(entrypoint, id)

			for _, alias := range aliases {
				// Create unique key for this alias+node combination
				key := fmt.Sprintf("%s:%s", alias, id.String())
				if !seenAliases[key] {
					seenAliases[key] = true
					dependencies = append(dependencies, Dependency{
						Name: alias,
						ID:   id,
						Node: node,
					})
				}
			}
		}
	}

	// Sort dependencies by dependency level (deeper dependencies first)
	sort.Slice(dependencies, func(i, j int) bool {
		levelI := m.getDependencyLevel(entrypoint, dependencies[i].ID)
		levelJ := m.getDependencyLevel(entrypoint, dependencies[j].ID)
		if levelI != levelJ {
			return levelI > levelJ // Deeper levels first
		}
		// If same level, sort by ID for consistent ordering
		return m.getIDStringInternal(dependencies[i].ID) < m.getIDStringInternal(dependencies[j].ID)
	})

	mainNode := m.nodes[entrypoint]
	return &Main{
		Main:         mainNode,
		Dependencies: dependencies,
	}, nil
}

// getDependencyLevel calculates the dependency level of a node relative to the entrypoint
// This method assumes the caller holds the appropriate lock.
func (m *MemoryGraph) getDependencyLevel(entrypoint, target registry.ID) int {
	if entrypoint == target {
		return 0
	}

	visited := make(map[registry.ID]bool)
	levels := make(map[registry.ID]int)
	queue := []registry.ID{entrypoint}
	levels[entrypoint] = 0
	visited[entrypoint] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		neighbors, err := m.graph.GetNeighbors(current)
		if err != nil {
			continue
		}

		for _, neighbor := range neighbors {
			if !visited[neighbor] {
				visited[neighbor] = true
				levels[neighbor] = levels[current] + 1
				queue = append(queue, neighbor)

				if neighbor == target {
					return levels[neighbor]
				}
			}
		}
	}

	return -1 // Not reachable
}

// findAllDependencyAliases finds all aliases for a dependency by tracing all dependency paths
// from the entrypoint to the target node. This method assumes the caller holds the appropriate lock.
func (m *MemoryGraph) findAllDependencyAliases(entrypoint, target registry.ID) []string {
	aliases := make([]string, 0)
	seenAliases := make(map[string]bool)

	// If it's a direct dependency, add the direct alias
	if edge, ok := m.graph.GetEdge(entrypoint, target); ok {
		alias := edge.Data.As
		if alias == "" {
			// Fallback to node name if alias is empty
			if node, exists := m.nodes[target]; exists {
				alias = node.ID.Name
			}
		}
		aliases = append(aliases, alias)
		seenAliases[alias] = true
	}

	// Find all paths to the target and collect unique aliases
	paths := m.findAllDependencyPaths(entrypoint, target)
	for _, path := range paths {
		if len(path) >= 2 {
			// Get the alias of the last edge in the path
			if edge, ok := m.graph.GetEdge(path[len(path)-2], path[len(path)-1]); ok {
				alias := edge.Data.As
				if alias == "" {
					// Fallback to node name if alias is empty
					if node, exists := m.nodes[target]; exists {
						alias = node.ID.Name
					}
				}
				if !seenAliases[alias] {
					aliases = append(aliases, alias)
					seenAliases[alias] = true
				}
			}
		}
	}

	// If no aliases found, fallback to node name
	if len(aliases) == 0 {
		if node, exists := m.nodes[target]; exists {
			aliases = append(aliases, node.ID.Name)
		}
	}

	return aliases
}

// findAllDependencyPaths finds all paths from entrypoint to target using BFS
// This method assumes the caller holds the appropriate lock.
func (m *MemoryGraph) findAllDependencyPaths(entrypoint, target registry.ID) [][]registry.ID {
	if entrypoint == target {
		return [][]registry.ID{{entrypoint}}
	}

	paths := make([][]registry.ID, 0)
	visited := make(map[registry.ID]bool)
	queue := []struct {
		node registry.ID
		path []registry.ID
	}{{entrypoint, []registry.ID{entrypoint}}}
	visited[entrypoint] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		neighbors, err := m.graph.GetNeighbors(current.node)
		if err != nil {
			continue
		}

		for _, neighbor := range neighbors {
			if neighbor == target {
				// Found a path to target
				newPath := make([]registry.ID, len(current.path))
				copy(newPath, current.path)
				newPath = append(newPath, neighbor)
				paths = append(paths, newPath)
			} else if !visited[neighbor] {
				visited[neighbor] = true
				newPath := make([]registry.ID, len(current.path))
				copy(newPath, current.path)
				newPath = append(newPath, neighbor)
				queue = append(queue, struct {
					node registry.ID
					path []registry.ID
				}{neighbor, newPath})
			}
		}
	}

	return paths
}

// findDependencyAlias finds the alias for a dependency by tracing the dependency path
// from the entrypoint to the target node. This method assumes the caller holds the appropriate lock.
func (m *MemoryGraph) findDependencyAlias(entrypoint, target registry.ID) string {
	// If it's a direct dependency, return the edge alias
	if edge, ok := m.graph.GetEdge(entrypoint, target); ok {
		return edge.Data.As
	}

	// For transitive dependencies, find the path and use the last alias in the path
	path := m.findDependencyPath(entrypoint, target)
	if len(path) >= 2 {
		// Return the alias of the last edge in the path
		if edge, ok := m.graph.GetEdge(path[len(path)-2], path[len(path)-1]); ok {
			return edge.Data.As
		}
	}

	// Fallback to node name if no alias found
	if node, exists := m.nodes[target]; exists {
		return node.ID.Name
	}

	return ""
}

// findDependencyPath finds a path from entrypoint to target using BFS
// This method assumes the caller holds the appropriate lock.
func (m *MemoryGraph) findDependencyPath(entrypoint, target registry.ID) []registry.ID {
	if entrypoint == target {
		return []registry.ID{entrypoint}
	}

	visited := make(map[registry.ID]bool)
	parent := make(map[registry.ID]registry.ID)
	queue := []registry.ID{entrypoint}
	visited[entrypoint] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		neighbors, err := m.graph.GetNeighbors(current)
		if err != nil {
			continue
		}

		for _, neighbor := range neighbors {
			if !visited[neighbor] {
				visited[neighbor] = true
				parent[neighbor] = current
				queue = append(queue, neighbor)

				if neighbor == target {
					// Reconstruct path
					path := []registry.ID{target}
					current := target
					for current != entrypoint {
						current = parent[current]
						path = append([]registry.ID{current}, path...)
					}
					return path
				}
			}
		}
	}

	return nil
}

// reachableFrom returns a set of all nodes reachable from the given entrypoint.
// This method assumes the caller already holds the appropriate lock.
func (m *MemoryGraph) reachableFrom(entrypoint registry.ID) map[registry.ID]bool {
	visited := make(map[registry.ID]bool)
	inStack := make(map[registry.ID]bool) // Track nodes in current recursion stack

	var dfs func(registry.ID) bool
	dfs = func(current registry.ID) bool {
		if inStack[current] {
			// Found a cycle, but we'll continue to avoid infinite recursion
			return false
		}
		if visited[current] {
			return false
		}

		visited[current] = true
		inStack[current] = true
		defer func() { inStack[current] = false }()

		neighbors, err := m.graph.GetNeighbors(current)
		if err != nil {
			return false
		}

		for _, neighbor := range neighbors {
			dfs(neighbor)
		}
		return false
	}

	dfs(entrypoint)
	return visited
}
