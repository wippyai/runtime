package lua

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
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

	// Dependency represents a named Lua function prototype.
	Dependency struct {
		Name string
		Node *Node
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
	graph *graph.Graph[registry.ID, Edge]
	nodes map[registry.ID]*Node
}

// NewMemoryGraph creates a new MemoryGraph instance.
func NewMemoryGraph() *MemoryGraph {
	return &MemoryGraph{
		graph: graph.New[registry.ID, Edge](),
		nodes: make(map[registry.ID]*Node),
	}
}

// AddNode inserts a new node into the graph.
func (m *MemoryGraph) AddNode(n *Node) error {
	if n == nil {
		return fmt.Errorf("node cannot be nil")
	}
	if _, exists := m.nodes[n.ID]; exists {
		return fmt.Errorf("node with ID %v already exists", n.ID)
	}

	m.graph.AddNode(n.ID)
	m.nodes[n.ID] = n
	return nil
}

// RemoveNode deletes a node and its associated edges from the graph.
// It returns an error if the node has any direct outgoing dependencies or incoming dependents.
func (m *MemoryGraph) RemoveNode(id registry.ID) error {
	if _, exists := m.nodes[id]; !exists {
		return fmt.Errorf("node with ID %v not found", id)
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
	return nil
}

// AddDependency creates a dependency edge from the node with ID 'from' to the node with ID 'to'.
func (m *MemoryGraph) AddDependency(from, to registry.ID, alias string) error {
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

	// Clone the graph and simulate the addition for cycle detection.
	tmp := m.graph.Clone()
	tmp.AddEdge(from, to, 1, Edge{As: ""})
	if _, err := tmp.DependencyLevels(); err != nil {
		return fmt.Errorf("adding dependency would create a cycle: %w", err)
	}
	m.graph.AddEdge(from, to, 1, Edge{As: alias})
	return nil
}

// RemoveDependency removes the dependency edge from 'from' to 'to'.
func (m *MemoryGraph) RemoveDependency(from, to registry.ID) error {
	if !m.graph.HasEdge(from, to) {
		return fmt.Errorf("dependency from %v to %v not found", from, to)
	}
	return m.graph.RemoveEdge(from, to)
}

// GetNode retrieves the node with the specified ID.
func (m *MemoryGraph) GetNode(id registry.ID) (*Node, error) {
	n, exists := m.nodes[id]
	if !exists {
		return nil, fmt.Errorf("node with ID %v not found", id)
	}
	return n, nil
}

// GetDirectDependencies returns all nodes that the node with the given ID depends on. Only direct dependencies are returned.
func (m *MemoryGraph) GetDirectDependencies(id registry.ID) ([]*Node, error) {
	if _, exists := m.nodes[id]; !exists {
		return nil, fmt.Errorf("node with ID %v not found", id)
	}
	neighborIDs, err := m.graph.GetNeighbors(id)
	if err != nil {
		return nil, err
	}
	var deps []*Node
	for _, nid := range neighborIDs {
		if node, ok := m.nodes[nid]; ok {
			deps = append(deps, node)
		}
	}
	return deps, nil
}

// GetDirectDependents returns all nodes that depend on the node with the specified ID. Only direct dependents are returned.
func (m *MemoryGraph) GetDirectDependents(id registry.ID) ([]*Node, error) {
	if _, exists := m.nodes[id]; !exists {
		return nil, fmt.Errorf("node with ID %v not found", id)
	}
	var dependents []*Node
	for nid, node := range m.nodes {
		if m.graph.HasEdge(nid, id) {
			dependents = append(dependents, node)
		}
	}
	return dependents, nil
}

// GetAllDependents returns all nodes that depend on the node with the specified ID, including transitive dependents.
// GetAllDependents returns all nodes that depend on the node with the specified ID, including transitive dependents.
func (m *MemoryGraph) GetAllDependents(id registry.ID) ([]*Node, error) {
	if _, exists := m.nodes[id]; !exists {
		return nil, fmt.Errorf("node with ID %v not found", id)
	}

	// Track both visited (for traversal) and added (for results)
	visited := make(map[registry.ID]bool)
	added := make(map[registry.ID]bool)
	var dependents []*Node

	var traverse func(currentID registry.ID) error
	traverse = func(currentID registry.ID) error {
		// Skip if already visited in traversal
		if visited[currentID] {
			return nil
		}
		visited[currentID] = true

		// Get direct dependents
		direct, err := m.GetDirectDependents(currentID)
		if err != nil {
			return err
		}

		// Add to results if not already added
		for _, dep := range direct {
			if !added[dep.ID] {
				dependents = append(dependents, dep)
				added[dep.ID] = true
			}
		}

		// Recursively traverse each dependent
		for _, dep := range direct {
			if err := traverse(dep.ID); err != nil {
				return err
			}
		}

		return nil
	}

	if err := traverse(id); err != nil {
		return nil, err
	}

	return dependents, nil
}

// DependencyLevels returns the nodes grouped in topological order (levels).
func (m *MemoryGraph) DependencyLevels() ([][]*Node, error) {
	gl, err := m.graph.DependencyLevels()
	if err != nil {
		return nil, err
	}
	var levels [][]*Node
	for i := 0; i < gl.LevelCount(); i++ {
		levelIDs, err := gl.GetLevel(i)
		if err != nil {
			return nil, err
		}
		var level []*Node
		for _, id := range levelIDs {
			if node, ok := m.nodes[id]; ok {
				level = append(level, node)
			}
		}
		// Sort for consistent ordering.
		sort.Slice(level, func(i, j int) bool {
			return fmt.Sprintf("%v", level[i].ID) < fmt.Sprintf("%v", level[j].ID)
		})
		levels = append(levels, level)
	}
	return levels, nil
}

// Build resolves dependencies starting from the entrypoint node and builds a Main configuration.
// The entrypoint becomes the main node, and all other reachable nodes are wrapped as dependency prototypes.
// If an incoming dependency edge carries a non‑empty alias, that alias is used.
func (m *MemoryGraph) Build(entrypoint registry.ID) (*Main, error) {
	entryNode, err := m.GetNode(entrypoint)
	if err != nil {
		return nil, err
	}
	levels, err := m.DependencyLevels()
	if err != nil {
		return nil, err
	}

	// Determine reachable nodes from the entrypoint
	reachable := m.reachableFrom(entrypoint)

	// Process levels in reverse order (deepest dependencies first)
	var ordered []*Node
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

	// Build map of all aliases for each node
	aliasMap := make(map[registry.ID]map[string]bool)
	for _, node := range ordered {
		if node.ID == entrypoint {
			continue
		}
		// Initialize alias map for this node
		aliasMap[node.ID] = make(map[string]bool)

		// Look through all nodes that could depend on this one
		for _, potentialParent := range ordered {
			if m.graph.HasEdge(potentialParent.ID, node.ID) {
				edge, _ := m.graph.GetEdge(potentialParent.ID, node.ID)
				if edge.Data.As != "" {
					aliasMap[node.ID][edge.Data.As] = true
				}
			}
		}
	}

	// Track modules we've already processed to avoid duplicates
	processedModules := make(map[string]bool)

	// Create dependency nodes in correct order
	var depNodes []Dependency
	for _, node := range ordered {
		if node.ID == entrypoint {
			continue
		}

		// Get all aliases for this node
		aliases := make([]string, 0)
		for alias := range aliasMap[node.ID] {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases) // Sort for consistent ordering

		// Process explicit aliases
		if len(aliases) > 0 {
			// Add node once for each unique alias
			for _, alias := range aliases {
				depNodes = append(depNodes, Dependency{
					Name: alias,
					Node: node,
				})
			}
			// Mark module as processed if present
			if node.Module != nil {
				processedModules[node.Module.Name()] = true
			}
		} else {
			// No aliases found and no module, add node with empty alias
			depNodes = append(depNodes, Dependency{
				Name: node.ID.Name,
				Node: node,
			})
		}
	}

	rt.Dependencies = depNodes
	return &rt, nil
}

// reachableFrom performs a DFS starting from the given entrypoint and returns a map of reachable node IDs.
func (m *MemoryGraph) reachableFrom(entrypoint registry.ID) map[registry.ID]bool {
	visited := make(map[registry.ID]bool)
	var dfs func(id registry.ID)
	dfs = func(id registry.ID) {
		if visited[id] {
			return
		}
		visited[id] = true
		neighbors, err := m.graph.GetNeighbors(id)
		if err != nil {
			return
		}
		for _, neighbor := range neighbors {
			dfs(neighbor)
		}
	}
	dfs(entrypoint)
	return visited
}
