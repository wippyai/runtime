package version

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/graph"
	"sort"
)

type versionMap struct {
	versions map[uint]registry.Version
}

func NewVersions() registry.VersionHistory {
	return &versionMap{
		versions: make(map[uint]registry.Version),
	}
}

func (vm *versionMap) Path(from, to registry.Version) ([]registry.Version, error) {
	if _, ok := vm.versions[from.ID()]; !ok {
		return nil, fmt.Errorf("version %v not found", from.ID())
	}
	if _, ok := vm.versions[to.ID()]; !ok {
		return nil, fmt.Errorf("version %v not found", to.ID())
	}

	fromID := from.ID()
	toID := to.ID()

	if fromID == toID {
		return []registry.Version{from}, nil
	}

	// Construct the graph on demand
	g := graph.NewGraph()
	for id, v := range vm.versions {
		g.AddNode(graph.Node(fmt.Sprintf("%v", id)))
		if prevID := v.PreviousID(); prevID != 0 {
			g.AddEdge(graph.Edge{
				From:   graph.Node(fmt.Sprintf("%v", prevID)),
				To:     graph.Node(fmt.Sprintf("%v", id)),
				Weight: 1,
			})

			g.AddEdge(graph.Edge{
				From:   graph.Node(fmt.Sprintf("%v", id)),
				To:     graph.Node(fmt.Sprintf("%v", prevID)),
				Weight: 2,
			})
		}
	}

	// Find the shortest path using the graph
	path, err := g.ShortestPath(
		graph.Node(fmt.Sprintf("%v", fromID)),
		graph.Node(fmt.Sprintf("%v", toID)),
	)
	if err != nil {
		return nil, err
	}

	// Convert the graph path to a version path
	versionPath := make([]registry.Version, len(path.Nodes))
	for i, node := range path.Nodes {
		var nodeID uint
		_, err := fmt.Sscan(string(node), &nodeID)
		if err != nil {
			return nil, err
		}

		versionPath[i] = vm.versions[nodeID]
	}

	return versionPath, nil
}

func (vm *versionMap) Add(v registry.Version) error {
	if _, ok := vm.versions[v.ID()]; ok {
		return fmt.Errorf("version %v already exists", v.ID())
	}

	vm.versions[v.ID()] = v
	return nil
}

func (vm *versionMap) Len() int {
	return len(vm.versions)
}

func (vm *versionMap) Range(f func(uint, registry.Version) bool) {
	// Collect versions into a slice for sorting
	var versions []registry.Version
	for _, v := range vm.versions {
		versions = append(versions, v)
	}

	// Sort the versions
	sort.Slice(versions, func(i, j int) bool {
		vi, vj := versions[i], versions[j]
		return vi.ID() < vj.ID()
	})

	// Iterate over the sorted versions
	for _, v := range versions {
		if !f(v.ID(), v) {
			break
		}
	}
}
