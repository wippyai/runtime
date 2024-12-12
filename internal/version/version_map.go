package version

import (
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/graph"
)

type versionMap struct {
	versions map[string]registry.Version
}

func NewVersions() registry.Versions {
	return &versionMap{
		versions: make(map[string]registry.Version),
	}
}

func (vm *versionMap) Path(from, to registry.Version) []registry.Version {
	if _, ok := vm.versions[from.ID()]; !ok {
		return nil
	}
	if _, ok := vm.versions[to.ID()]; !ok {
		return nil
	}

	fromID := from.ID()
	toID := to.ID()

	if fromID == toID {
		return []registry.Version{from}
	}

	// Construct the graph on demand
	g := graph.NewGraph()
	for id, v := range vm.versions {
		g.AddNode(graph.Node(id))
		if prevID := v.PreviousID(); prevID != "" {
			g.AddEdge(graph.Edge{
				From:   graph.Node(prevID),
				To:     graph.Node(id),
				Weight: 1,
			})
			g.AddEdge(graph.Edge{
				From:   graph.Node(id),
				To:     graph.Node(prevID),
				Weight: 1,
			})
		}
	}

	// Find the shortest path using the graph
	path, err := g.ShortestPath(graph.Node(fromID), graph.Node(toID))
	if err != nil {
		return nil // No path found or error
	}

	// Convert the graph path to a version path
	versionPath := make([]registry.Version, len(path.Nodes))
	for i, node := range path.Nodes {
		versionPath[i] = vm.versions[string(node)]
	}

	return versionPath
}

func (vm *versionMap) Add(v registry.Version) {
	vm.versions[v.ID()] = v
}

func (vm *versionMap) Get(id string) (registry.Version, bool) {
	v, ok := vm.versions[id]
	return v, ok
}

func (vm *versionMap) Delete(id string) {
	delete(vm.versions, id)
}

func (vm *versionMap) Len() int {
	return len(vm.versions)
}

func (vm *versionMap) Range(f func(string, registry.Version) bool) {
	for id, v := range vm.versions {
		if !f(id, v) {
			break
		}
	}
}
