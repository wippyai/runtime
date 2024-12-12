package version

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/graph"
	"sort"
)

type versionMap struct {
	versions map[string]registry.Version
}

func NewVersions() registry.VersionHistory {
	return &versionMap{
		versions: make(map[string]registry.Version),
	}
}

func (vm *versionMap) Path(from, to registry.Version) ([]registry.Version, error) {
	if _, ok := vm.versions[from.ID()]; !ok {
		return nil, fmt.Errorf("version %s not found", from.ID())
	}
	if _, ok := vm.versions[to.ID()]; !ok {
		return nil, fmt.Errorf("version %s not found", to.ID())
	}

	fromID := from.ID()
	toID := to.ID()

	if fromID == toID {
		return []registry.Version{from}, nil
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
				Weight: 2,
			})
		}
	}

	// Find the shortest path using the graph
	path, err := g.ShortestPath(graph.Node(fromID), graph.Node(toID))
	if err != nil {
		return nil, err
	}

	// Convert the graph path to a version path
	versionPath := make([]registry.Version, len(path.Nodes))
	for i, node := range path.Nodes {
		versionPath[i] = vm.versions[string(node)]
	}

	return versionPath, nil
}

func (vm *versionMap) Add(v registry.Version) error {
	if _, ok := vm.versions[v.ID()]; ok {
		return fmt.Errorf("version %s already exists", v.ID())
	}

	vm.versions[v.ID()] = v
	return nil
}

func (vm *versionMap) Len() int {
	return len(vm.versions)
}

func (vm *versionMap) Range(f func(string, registry.Version) bool) {
	// Collect versions into a slice for sorting
	var versions []registry.Version
	for _, v := range vm.versions {
		versions = append(versions, v)
	}

	// Sort the versions
	sort.Slice(versions, func(i, j int) bool {
		vi, vj := versions[i], versions[j]
		if vi.Major() != vj.Major() {
			return vi.Major() < vj.Major()
		}
		if vi.Minor() != vj.Minor() {
			return vi.Minor() < vj.Minor()
		}
		return vi.ID() < vj.ID() // Assuming ID is comparable
	})

	// Iterate over the sorted versions
	for _, v := range versions {
		if !f(v.ID(), v) {
			break
		}
	}
}
