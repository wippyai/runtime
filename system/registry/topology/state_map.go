package topology

import (
	"slices"
	"strings"

	"github.com/ponyruntime/pony/api/registry"
)

// StateMap is a representation of the registry state using a map for faster lookups.
type StateMap map[registry.ID]registry.Entry

// NewStateMap creates a new StateMap from a registry.State
func NewStateMap(state registry.State) StateMap {
	m := make(StateMap)
	for _, entry := range state {
		m[entry.ID] = entry
	}
	return m
}

// Copy creates a shallow copy of the StateMap.
func (sm StateMap) Copy() StateMap {
	newMap := make(StateMap)
	for k, v := range sm {
		newMap[k] = v
	}
	return newMap
}

// ToSlice converts a StateMap (map) to a State (slice).
func (sm StateMap) ToSlice() registry.State {
	slice := make(registry.State, 0, len(sm))
	for _, entry := range sm {
		slice = append(slice, entry)
	}
	// Sort the slice by ID (namespace first, then name)
	slices.SortFunc(slice, func(a, b registry.Entry) int {
		// First compare by namespace
		if nsComp := strings.Compare(a.ID.NS, b.ID.NS); nsComp != 0 {
			return nsComp
		}

		// If namespaces are equal, compare by name
		return strings.Compare(a.ID.Name, b.ID.Name)
	})
	return slice
}
