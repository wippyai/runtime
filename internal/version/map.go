package version

import (
	"fmt"
	"sort"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/graph"
)

type (
	Map interface {
		// Path returns the ordered sequence of version instances connecting a starting
		// version ('from') and an ending version ('to'), including both 'from' and 'to'.
		//
		// Constraints:
		//   - Both 'from' and 'to' MUST exist; otherwise, nil is returned.
		//   - If 'from' and 'to' are identical, the returned slice contains only 'from'.
		//   - If no valid path exists, nil is returned.
		Path(from, to registry.Version) ([]registry.Version, error)

		// Len returns the number of versions.
		Len() int

		// Add adds a new version. Duplicate versions not allowed.
		Add(v registry.Version) error

		// Range iterates over the versions.
		Range(f func(id uint, v registry.Version) bool)
	}
)

type versionHistory struct {
	versions map[string]registry.Version
}

func NewVersionMap() Map {
	return &versionHistory{
		versions: make(map[string]registry.Version),
	}
}

func (vm *versionHistory) Path(from, to registry.Version) ([]registry.Version, error) {
	if _, ok := vm.versions[from.String()]; !ok {
		return nil, fmt.Errorf("version %v not found", from.ID())
	}
	if _, ok := vm.versions[to.String()]; !ok {
		return nil, fmt.Errorf("version %v not found", to.ID())
	}

	if from.ID() == to.ID() {
		return []registry.Version{from}, nil
	}

	// Construct the graph on demand
	g := graph.NewGraph()
	for _, v := range vm.versions {
		g.AddNode(graph.Node(v.String()))
		if prev := v.Previous(); prev != nil {
			g.AddEdge(graph.Edge{
				From:   graph.Node(prev.String()),
				To:     graph.Node(v.String()),
				Weight: 1,
			})

			g.AddEdge(graph.Edge{
				From:   graph.Node(v.String()),
				To:     graph.Node(prev.String()),
				Weight: 2,
			})
		}
	}

	// Find the shortest path using the graph
	path, err := g.ShortestPath(
		graph.Node(from.String()),
		graph.Node(to.String()),
	)
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

func (vm *versionHistory) Add(v registry.Version) error {
	if _, ok := vm.versions[v.String()]; ok {
		return fmt.Errorf("version %v already exists", v.ID())
	}

	vm.versions[v.String()] = v
	return nil
}

func (vm *versionHistory) Len() int {
	return len(vm.versions)
}

func (vm *versionHistory) Range(f func(uint, registry.Version) bool) {
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
