package version

import (
	"fmt"
	"sort"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/graph"
)

type (
	// Map represents a version history that maintains relationships between different
	// versions and provides operations to traverse and manage the version graph.
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
		Range(f func(id string, v registry.Version) bool)
	}
)

type versionHistory struct {
	versions map[string]registry.Version
}

// NewVersionMap creates a new empty version map for tracking version history
// and relationships between different versions. It initializes the internal
// storage and provides thread-safe access to version information.
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

	// todo: can be optimized as `typically` we always event source from root

	// Construct the graph on demand
	g := graph.New[string, any]()
	for _, v := range vm.versions {
		g.AddNode(v.String())
		if prev := v.Previous(); prev != nil {
			// AddCleanup bidirectional edges with different weights
			g.AddEdge(prev.String(), v.String(), 1, nil) // Forward edge
			g.AddEdge(v.String(), prev.String(), 2, nil) // Backward edge
		}
	}

	// Find the shortest path using the graph
	path, err := g.ShortestPath(from.String(), to.String())
	if err != nil {
		return nil, err
	}

	// Convert the graph path to a version path
	versionPath := make([]registry.Version, len(path.Nodes))
	for i, node := range path.Nodes {
		versionPath[i] = vm.versions[node]
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

func (vm *versionHistory) Range(f func(string, registry.Version) bool) {
	// Collect versions into a slice for sorting
	versions := make([]registry.Version, 0, len(vm.versions))
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
