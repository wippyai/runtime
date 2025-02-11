package lua

import (
	"fmt"
	"sort"

	"github.com/ponyruntime/pony/api/registry"
	runtime "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/internal/graph"
)

// MemoryGraph is an in‑memory implementation of the CodeGraph interface.
// It maintains nodes (representing code units/modules) and their dependency edges.
// Dependency edges (of type runtime.Edge) carry an alias, which is later propagated into
// the final runtime configuration as part of the runtime.AliasedNode wrapper.
type MemoryGraph struct {
	graph *graph.Graph[registry.ID, runtime.Edge]
	nodes map[registry.ID]*runtime.Node
}

// NewMemoryGraph creates a new MemoryGraph instance.
func NewMemoryGraph() *MemoryGraph {
	return &MemoryGraph{
		graph: graph.New[registry.ID, runtime.Edge](),
		nodes: make(map[registry.ID]*runtime.Node),
	}
}

// AddNode inserts a new node into the graph.
func (m *MemoryGraph) AddNode(n *runtime.Node) error {
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

	// Clone the graph and simulate the addition for cycle detection.
	tmp := m.graph.Clone()
	tmp.AddEdge(from, to, 1, runtime.Edge{Alias: ""})
	if _, err := tmp.DependencyLevels(); err != nil {
		return fmt.Errorf("adding dependency would create a cycle: %w", err)
	}
	m.graph.AddEdge(from, to, 1, runtime.Edge{Alias: alias})
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
func (m *MemoryGraph) GetNode(id registry.ID) (*runtime.Node, error) {
	n, exists := m.nodes[id]
	if !exists {
		return nil, fmt.Errorf("node with ID %v not found", id)
	}
	return n, nil
}

// GetDirectDependencies returns all nodes that the node with the given ID depends on. Only direct dependencies are returned.
func (m *MemoryGraph) GetDirectDependencies(id registry.ID) ([]*runtime.Node, error) {
	if _, exists := m.nodes[id]; !exists {
		return nil, fmt.Errorf("node with ID %v not found", id)
	}
	neighborIDs, err := m.graph.GetNeighbors(id)
	if err != nil {
		return nil, err
	}
	var deps []*runtime.Node
	for _, nid := range neighborIDs {
		if node, ok := m.nodes[nid]; ok {
			deps = append(deps, node)
		}
	}
	return deps, nil
}

// GetDirectDependents returns all nodes that depend on the node with the specified ID. Only direct dependents are returned.
func (m *MemoryGraph) GetDirectDependents(id registry.ID) ([]*runtime.Node, error) {
	if _, exists := m.nodes[id]; !exists {
		return nil, fmt.Errorf("node with ID %v not found", id)
	}
	var dependents []*runtime.Node
	for nid, node := range m.nodes {
		if m.graph.HasEdge(nid, id) {
			dependents = append(dependents, node)
		}
	}
	return dependents, nil
}

// DependencyLevels returns the nodes grouped in topological order (levels).
func (m *MemoryGraph) DependencyLevels() ([][]*runtime.Node, error) {
	gl, err := m.graph.DependencyLevels()
	if err != nil {
		return nil, err
	}
	var levels [][]*runtime.Node
	for i := 0; i < gl.LevelCount(); i++ {
		levelIDs, err := gl.GetLevel(i)
		if err != nil {
			return nil, err
		}
		var level []*runtime.Node
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
func (m *MemoryGraph) Build(entrypoint registry.ID) (runtime.Main, error) {
	entryNode, err := m.GetNode(entrypoint)
	if err != nil {
		return runtime.Main{}, err
	}
	levels, err := m.DependencyLevels()
	if err != nil {
		return runtime.Main{}, err
	}
	// Determine reachable nodes from the entrypoint.
	reachable := m.reachableFrom(entrypoint)
	var ordered []*runtime.Node
	for _, level := range levels {
		for _, node := range level {
			if reachable[node.ID] {
				ordered = append(ordered, node)
			}
		}
	}
	// Assemble the runtime.
	rt := runtime.Main{
		Main: entryNode,
	}
	var depNodes []runtime.AliasedNode
	for i, node := range ordered {
		if node.ID == entrypoint {
			continue
		}
		alias := ""
		// Check earlier nodes (parents) for an incoming edge with a non‑empty alias.
		for j := 0; j < i; j++ {
			parent := ordered[j]
			if m.graph.HasEdge(parent.ID, node.ID) {
				edge, _ := m.graph.GetEdge(parent.ID, node.ID)
				if edge.Data.Alias != "" {
					alias = edge.Data.Alias
					break
				}
			}
		}
		depNodes = append(depNodes, runtime.AliasedNode{Alias: alias, Node: node})
	}
	rt.DepProtos = depNodes

	// Collect unique modules from all nodes.
	modMap := make(map[*runtime.Module]bool)
	for _, node := range ordered {
		if node.Module != nil {
			modMap[node.Module] = true
		}
	}
	for mod := range modMap {
		rt.Modules = append(rt.Modules, mod)
	}
	return rt, nil
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
