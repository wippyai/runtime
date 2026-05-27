// SPDX-License-Identifier: MPL-2.0

package code

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	glua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/bytecode"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/api/registry"
	runtime "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/internal/graph"
)

type (
	// Version tracks changes to nodes
	Version struct {
		Created  time.Time
		Hash     string
		Revision uint64
	}

	// Node represents a code unit in the dependency graph.
	Node struct {
		Module   *runtime.ModuleDef
		Manifest *io.Manifest
		ID       registry.ID
		Kind     registry.Kind
		Source   string
		Method   string
		Version  Version
	}

	Preload struct {
		Name     string
		ModuleID registry.ID
	}

	Dependency struct {
		Node *Node
		ID   registry.ID
		Name string
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

func HashNodeWithProto(node *Node, proto *glua.FunctionProto) string {
	h := sha256.New()
	h.Write([]byte(node.Method))
	if proto != nil {
		if data, err := bytecode.Dump(proto); err == nil {
			h.Write(data)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// MemoryGraph is an in‑memory implementation of the CodeGraph interface.
// It maintains nodes (representing code units/modules) and their dependency edges.
// Dependency edges (of type runtime.Edge) carry an alias, which is later propagated into
// the final runtime configuration as part of the runtime.Dependency wrapper.
type MemoryGraph struct {
	graph                 *graph.Graph[registry.ID, Edge]
	nodes                 map[registry.ID]*Node
	dependentsCache       map[registry.ID][]*Node
	dependencyLevelsCache [][]*Node
	mu                    sync.RWMutex
	cacheValid            bool
}

// NewMemoryGraph creates a new MemoryGraph instance.
func NewMemoryGraph() *MemoryGraph {
	return &MemoryGraph{
		graph:           graph.New[registry.ID, Edge](),
		nodes:           make(map[registry.ID]*Node),
		dependentsCache: make(map[registry.ID][]*Node),
		cacheValid:      true,
	}
}

func (m *MemoryGraph) Snapshot() *MemoryGraph {
	m.mu.RLock()
	defer m.mu.RUnlock()

	nodes := make(map[registry.ID]*Node, len(m.nodes))
	for id, node := range m.nodes {
		nodes[id] = cloneNode(node)
	}
	return &MemoryGraph{
		graph:           m.graph.Clone(),
		nodes:           nodes,
		dependentsCache: make(map[registry.ID][]*Node),
		cacheValid:      true,
	}
}

func (m *MemoryGraph) SetManifestIfRevision(id registry.ID, revision uint64, manifest *io.Manifest) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	node, exists := m.nodes[id]
	if !exists || node.Version.Revision != revision {
		return false
	}
	node.Manifest = manifest
	return true
}

// invalidateCacheLocked marks all caches as invalid. Must be called with mu held.
func (m *MemoryGraph) invalidateCacheLocked() {
	m.cacheValid = false
	m.dependencyLevelsCache = nil
	for k := range m.dependentsCache {
		delete(m.dependentsCache, k)
	}
}

// hasCycle performs DFS-based cycle detection without cloning the graph
func (m *MemoryGraph) hasCycle(from, to registry.ID) bool {
	if from == to {
		return true
	}

	visited := make(map[registry.ID]bool)
	inStack := make(map[registry.ID]bool)

	var dfs func(registry.ID) bool
	dfs = func(current registry.ID) bool {
		if inStack[current] {
			return true // Found cycle
		}
		if visited[current] {
			return false // Already explored this path
		}

		visited[current] = true
		inStack[current] = true

		// Check if we can reach 'from' through any neighbor
		neighbors, err := m.graph.GetNeighbors(current)
		if err != nil {
			return false
		}

		for _, neighbor := range neighbors {
			if neighbor == from || dfs(neighbor) {
				return true
			}
		}

		inStack[current] = false
		return false
	}

	return dfs(to)
}

// AddNode inserts a new node into the graph.
func (m *MemoryGraph) AddNode(n *Node) error {
	if n == nil {
		return ErrNodeNil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.nodes[n.ID]; exists {
		return NewNodeExistsError(n.ID)
	}

	m.graph.AddNode(n.ID)
	m.nodes[n.ID] = n
	m.invalidateCacheLocked()
	return nil
}

// UpdateNode replaces an existing node's content and full dependency set
// atomically. The graph is left unchanged if any dependency validation fails.
func (m *MemoryGraph) UpdateNode(n *Node, deps []Import) error {
	if n == nil {
		return ErrNodeNil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.nodes[n.ID]; !exists {
		return NewNodeNotFoundError(n.ID)
	}

	seenDeps := make(map[registry.ID]bool, len(deps))
	aliases := make(map[string]registry.ID, len(deps))
	for _, dep := range deps {
		if _, exists := m.nodes[dep.ID]; !exists {
			return NewNodeNotFoundError(dep.ID)
		}
		if seenDeps[dep.ID] {
			return NewDependencyExistsError(n.ID, dep.ID)
		}
		seenDeps[dep.ID] = true

		if dep.Alias != "" {
			if existing, exists := aliases[dep.Alias]; exists && existing != dep.ID {
				return NewAliasCollisionError(dep.Alias, n.ID, existing, true, dep.ID, true)
			}
			aliases[dep.Alias] = dep.ID
		}

		if m.hasCycle(n.ID, dep.ID) {
			return ErrCycleDetected
		}
	}

	nextGraph := m.graph.Clone()
	oldDeps, err := nextGraph.GetNeighbors(n.ID)
	if err != nil {
		return err
	}
	for _, oldDep := range oldDeps {
		if err := nextGraph.RemoveEdge(n.ID, oldDep); err != nil {
			return err
		}
	}
	for _, dep := range deps {
		nextGraph.AddEdge(n.ID, dep.ID, 1, Edge{As: dep.Alias})
	}

	m.graph = nextGraph
	m.nodes[n.ID] = n
	m.invalidateCacheLocked()
	return nil
}

// RemoveNode deletes a node and its associated edges from the graph.
// It returns an error if the node has any direct outgoing dependencies or incoming dependents.
func (m *MemoryGraph) RemoveNode(id registry.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.nodes[id]; !exists {
		return NewNodeNotFoundError(id)
	}

	// Check for incoming dependencies.
	for nid := range m.nodes {
		if m.graph.HasEdge(nid, id) {
			return NewIncomingDependencyError(id, nid)
		}
	}

	if err := m.graph.RemoveNode(id); err != nil {
		return err
	}

	delete(m.nodes, id)
	m.invalidateCacheLocked()
	return nil
}

// AddDependency creates a dependency edge from the node with Process 'from' to the node with Process 'to'.
func (m *MemoryGraph) AddDependency(from, to registry.ID, alias string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.nodes[from]; !exists {
		return NewNodeNotFoundError(from)
	}
	if _, exists := m.nodes[to]; !exists {
		return NewNodeNotFoundError(to)
	}

	if m.graph.HasEdge(from, to) {
		return NewDependencyExistsError(from, to)
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
					return NewAliasCollisionError(alias, from, neighbor, true, to, true)
				}
			}
		}
	}

	// Efficient cycle detection without graph cloning
	if m.hasCycle(from, to) {
		return ErrCycleDetected
	}

	m.graph.AddEdge(from, to, 1, Edge{As: alias})
	m.invalidateCacheLocked()
	return nil
}

// RemoveDependency removes the dependency edge from 'from' to 'to'.
func (m *MemoryGraph) RemoveDependency(from, to registry.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.graph.HasEdge(from, to) {
		return NewDependencyNotFoundError(from, to)
	}
	m.invalidateCacheLocked()
	return m.graph.RemoveEdge(from, to)
}

// GetNode retrieves the node with the specified Process.
func (m *MemoryGraph) GetNode(id registry.ID) (*Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	n, exists := m.nodes[id]
	if !exists {
		return nil, NewNodeNotFoundError(id)
	}
	return n, nil
}

// GetDependenciesWithAliases returns direct dependencies with their aliases.
func (m *MemoryGraph) GetDependenciesWithAliases(id registry.ID) ([]Dependency, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, exists := m.nodes[id]; !exists {
		return nil, NewNodeNotFoundError(id)
	}

	neighborIDs, err := m.graph.GetNeighbors(id)
	if err != nil {
		return nil, err
	}

	deps := make([]Dependency, 0, len(neighborIDs))
	for _, nid := range neighborIDs {
		node, ok := m.nodes[nid]
		if !ok {
			continue
		}
		alias := ""
		if edge, ok := m.graph.GetEdge(id, nid); ok {
			alias = edge.Data.As
		}
		if alias == "" {
			alias = nid.Name
		}
		deps = append(deps, Dependency{
			Name: alias,
			ID:   nid,
			Node: node,
		})
	}
	return deps, nil
}

// GetDirectDependencies returns all nodes that the node with the given Process depends on. Only direct dependencies are returned.
func (m *MemoryGraph) GetDirectDependencies(id registry.ID) ([]*Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, exists := m.nodes[id]; !exists {
		return nil, NewNodeNotFoundError(id)
	}

	neighborIDs, err := m.graph.GetNeighbors(id)
	if err != nil {
		return nil, err
	}

	// Pre-allocate slice with known capacity
	deps := make([]*Node, 0, len(neighborIDs))
	for _, nid := range neighborIDs {
		if node, ok := m.nodes[nid]; ok {
			deps = append(deps, node)
		}
	}
	return deps, nil
}

// GetDirectDependents returns all nodes that depend on the node with the specified Process. Only direct dependents are returned.
func (m *MemoryGraph) GetDirectDependents(id registry.ID) ([]*Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, exists := m.nodes[id]; !exists {
		return nil, NewNodeNotFoundError(id)
	}

	// Pre-allocate with estimated capacity
	dependents := make([]*Node, 0, len(m.nodes)/10) // Reasonable estimate
	for nid, node := range m.nodes {
		if m.graph.HasEdge(nid, id) {
			dependents = append(dependents, node)
		}
	}
	return dependents, nil
}

// GetAllDependents returns all nodes that depend on the node with the specified Process, including transitive dependents.
func (m *MemoryGraph) GetAllDependents(id registry.ID) ([]*Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, exists := m.nodes[id]; !exists {
		return nil, NewNodeNotFoundError(id)
	}

	// Check cache first
	if m.cacheValid {
		if cached, exists := m.dependentsCache[id]; exists {
			return cached, nil
		}
	}

	// Track visited nodes and results efficiently
	visited := make(map[registry.ID]bool, len(m.nodes))
	resultSet := make(map[registry.ID]*Node) // Use map for O(1) deduplication
	queue := []registry.ID{id}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if visited[current] {
			continue
		}
		visited[current] = true

		// Find all direct dependents of current node
		for nid, node := range m.nodes {
			if !visited[nid] && m.graph.HasEdge(nid, current) {
				resultSet[nid] = node
				queue = append(queue, nid)
			}
		}
	}

	// Convert map to slice
	dependents := make([]*Node, 0, len(resultSet))
	for _, node := range resultSet {
		dependents = append(dependents, node)
	}

	// Cache the result (note: we're holding RLock so can't modify cache)
	// Caching here would require upgrading to Write lock which could deadlock

	return dependents, nil
}

// DependencyLevels returns the nodes grouped in topological order (levels).
func (m *MemoryGraph) DependencyLevels() ([][]*Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check cache first
	if m.cacheValid && m.dependencyLevelsCache != nil {
		return m.dependencyLevelsCache, nil
	}

	gl, err := m.graph.DependencyLevels()
	if err != nil {
		return nil, err
	}

	levels := make([][]*Node, 0, gl.LevelCount())
	for i := 0; i < gl.LevelCount(); i++ {
		levelIDs, err := gl.GetLevel(i)
		if err != nil {
			return nil, err
		}

		// Pre-allocate level slice
		level := make([]*Node, 0, len(levelIDs))
		for _, id := range levelIDs {
			if node, ok := m.nodes[id]; ok {
				level = append(level, node)
			}
		}

		sort.Slice(level, func(i, j int) bool {
			return level[i].ID.String() < level[j].ID.String()
		})

		levels = append(levels, level)
	}

	return levels, nil
}

// dependencyLevelsLocked is the internal version without locking.
func (m *MemoryGraph) dependencyLevelsLocked() ([][]*Node, error) {
	gl, err := m.graph.DependencyLevels()
	if err != nil {
		return nil, err
	}

	levels := make([][]*Node, 0, gl.LevelCount())
	for i := 0; i < gl.LevelCount(); i++ {
		levelIDs, err := gl.GetLevel(i)
		if err != nil {
			return nil, err
		}

		level := make([]*Node, 0, len(levelIDs))
		for _, id := range levelIDs {
			if node, ok := m.nodes[id]; ok {
				level = append(level, node)
			}
		}

		sort.Slice(level, func(i, j int) bool {
			return level[i].ID.String() < level[j].ID.String()
		})

		levels = append(levels, level)
	}

	return levels, nil
}

// reachableFromLocked performs a DFS starting from the given entrypoint and returns a map of reachable node IDs.
func (m *MemoryGraph) reachableFromLocked(entrypoint registry.ID) map[registry.ID]bool {
	visited := make(map[registry.ID]bool, len(m.nodes))
	queue := []registry.ID{entrypoint}

	for len(queue) > 0 {
		current := queue[len(queue)-1]
		queue = queue[:len(queue)-1]

		if visited[current] {
			continue
		}
		visited[current] = true

		neighbors, err := m.graph.GetNeighbors(current)
		if err != nil {
			continue
		}

		for _, neighbor := range neighbors {
			if !visited[neighbor] {
				queue = append(queue, neighbor)
			}
		}
	}

	return visited
}

// Build resolves dependencies starting from the entrypoint node and builds a Main configuration.
// The entrypoint becomes the main node, and all other reachable nodes are wrapped as dependency prototypes.
// If an incoming dependency edge carries a non‑empty alias, that alias is used.
// A node may appear multiple times in dependencies if different parents import it with different aliases.
func (m *MemoryGraph) Build(entrypoint registry.ID) (*Main, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entryNode, exists := m.nodes[entrypoint]
	if !exists {
		return nil, NewNodeNotFoundError(entrypoint)
	}

	levels, err := m.dependencyLevelsLocked()
	if err != nil {
		return nil, err
	}

	// Determine reachable nodes from the entrypoint
	reachable := m.reachableFromLocked(entrypoint)

	// Process levels in reverse order (deepest dependencies first)
	ordered := make([]*Node, 0, len(reachable))
	for i := len(levels) - 1; i >= 0; i-- {
		level := levels[i]
		for _, node := range level {
			if reachable[node.ID] {
				ordered = append(ordered, node)
			}
		}
	}

	// Assemble the runtime
	rt := Main{
		Main: entryNode,
	}

	// Pre-build edge map for efficient alias lookup (O(1) instead of O(n²))
	edgeMap := make(map[registry.ID]map[registry.ID]string) // from -> to -> alias
	for _, node := range ordered {
		neighbors, err := m.graph.GetNeighbors(node.ID)
		if err != nil {
			continue
		}
		edgeMap[node.ID] = make(map[registry.ID]string, len(neighbors))
		for _, neighbor := range neighbors {
			if edge, ok := m.graph.GetEdge(node.ID, neighbor); ok {
				edgeMap[node.ID][neighbor] = edge.Data.As
			}
		}
	}

	// Build alias map: collect ALL aliases for each node from ALL edges pointing to it
	// A node can have multiple aliases if different parents import it with different names
	aliasMap := make(map[registry.ID]map[string]bool)
	for _, node := range ordered {
		if node.ID.Equal(entrypoint) {
			continue
		}
		aliasMap[node.ID] = make(map[string]bool)

		// Collect all aliases from all edges pointing to this node
		for _, edgeTargets := range edgeMap {
			if alias, hasEdge := edgeTargets[node.ID]; hasEdge && alias != "" {
				aliasMap[node.ID][alias] = true
			}
		}
	}

	// Track modules we've already processed to avoid duplicates
	processedModules := make(map[string]bool)

	// Build dependency nodes in correct order
	depNodes := make([]Dependency, 0, len(ordered))
	for _, node := range ordered {
		if node.ID.Equal(entrypoint) {
			continue
		}

		// Get all aliases for this node, sorted for consistent ordering
		aliases := make([]string, 0, len(aliasMap[node.ID]))
		for alias := range aliasMap[node.ID] {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases)

		// Add dependency entry for each alias
		if len(aliases) > 0 {
			for _, alias := range aliases {
				depNodes = append(depNodes, Dependency{
					Name: alias,
					Node: node,
				})
			}
			// Mark module as processed if present
			if node.Module != nil {
				processedModules[node.Module.Info().Name] = true
			}
		} else {
			// No aliases - use node name as default
			depNodes = append(depNodes, Dependency{
				Name: node.ID.Name,
				Node: node,
			})
		}
	}

	// Validate no duplicate names pointing to different nodes
	nameToNode := make(map[string]registry.ID)
	for _, dep := range depNodes {
		if existingNodeID, exists := nameToNode[dep.Name]; exists {
			if !existingNodeID.Equal(dep.Node.ID) {
				return nil, NewAliasCollisionError(dep.Name, entrypoint, existingNodeID, false, dep.Node.ID, false)
			}
		}
		nameToNode[dep.Name] = dep.Node.ID
	}

	rt.Dependencies = depNodes
	return &rt, nil
}
