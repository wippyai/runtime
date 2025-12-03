package version

import (
	"sort"
	"sync"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/graph"
)

type (
	// Map represents a version history that maintains relationships between different
	// versions and provides operations to traverse and manage the version graph.
	Map interface {
		// Path returns the ordered sequence of version instances representing
		// the changesets that must be applied to transition from 'from' to 'to'.
		// The 'from' version is EXCLUDED (you're already at that state).
		// The 'to' version is INCLUDED (you want to reach that state).
		//
		// Example: Path(v0, v2) returns [v1, v2] - apply changesets v1 then v2
		//
		// Constraints:
		//   - Both 'from' and 'to' MUST exist; otherwise, error is returned.
		//   - If 'from' and 'to' are identical, returns empty slice (no changes).
		//   - If no valid path exists, error is returned.
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
	mu       sync.RWMutex
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
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	if _, ok := vm.versions[from.String()]; !ok {
		return nil, NewVersionNotFoundError(from.ID())
	}
	if _, ok := vm.versions[to.String()]; !ok {
		return nil, NewVersionNotFoundError(to.ID())
	}

	if from.ID() == to.ID() {
		// Already at target version, no changes needed
		return []registry.Version{}, nil
	}

	// Construct the graph on demand
	g := graph.New[string, any]()
	for _, v := range vm.versions {
		g.AddNode(v.String())
		if prev := v.Previous(); prev != nil {
			// Add bidirectional edges with different weights
			g.AddEdge(prev.String(), v.String(), 1, nil) // Forward edge
			g.AddEdge(v.String(), prev.String(), 2, nil) // Backward edge
		}
	}

	// Find the shortest path using the graph
	path, err := g.ShortestPath(from.String(), to.String())
	if err != nil {
		return nil, err
	}

	// Convert the graph path to a version path, excluding the starting version
	// Path represents the changesets to apply, not the states visited
	// Example: Path(v0, v2) returns [v1, v2] (apply changesets for v1 and v2)
	if len(path.Nodes) <= 1 {
		return []registry.Version{}, nil
	}

	versionPath := make([]registry.Version, len(path.Nodes)-1)
	for i, node := range path.Nodes[1:] { // Skip the first node (from)
		versionPath[i] = vm.versions[node]
	}

	return versionPath, nil
}

func (vm *versionHistory) Add(v registry.Version) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if _, ok := vm.versions[v.String()]; ok {
		return NewVersionAlreadyExistsError(v.ID())
	}

	vm.versions[v.String()] = v
	return nil
}

func (vm *versionHistory) Len() int {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	return len(vm.versions)
}

func (vm *versionHistory) Range(f func(uint, registry.Version) bool) {
	vm.mu.RLock()
	// Collect versions into a slice for sorting
	versions := make([]registry.Version, 0, len(vm.versions))
	for _, v := range vm.versions {
		versions = append(versions, v)
	}
	vm.mu.RUnlock()

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
