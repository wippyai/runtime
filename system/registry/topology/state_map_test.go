package topology

import (
	"reflect"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

func TestStateMap(t *testing.T) {
	// Sample entries for testing
	entry1 := registry.Entry{
		ID: registry.ID{
			NS:   "ns1",
			Name: "service.api.host",
		},
		Kind: "listener",
		Data: payload.NewString("localhost"),
	}
	entry2 := registry.Entry{
		ID: registry.ID{
			NS:   "ns1",
			Name: "service.api.port",
		},
		Kind: "listener",
		Data: payload.NewString("8080"),
	}

	initialState := registry.State{entry1, entry2}

	t.Run("NewStateMap", func(t *testing.T) {
		stateMap := NewStateMap(initialState)

		if len(stateMap) != len(initialState) {
			t.Errorf("NewStateMap() failed: expected map length %d, got %d", len(initialState), len(stateMap))
		}

		// Verify entries are correctly mapped by ID
		for _, entry := range initialState {
			if mappedEntry, exists := stateMap[entry.ID]; !exists {
				t.Errorf("NewStateMap() failed: entry with ID {ns: %s, name: %s} missing in map",
					entry.ID.NS, entry.ID.Name)
			} else if !reflect.DeepEqual(mappedEntry, entry) {
				t.Errorf("NewStateMap() failed: entry mismatch for ID {ns: %s, name: %s}",
					entry.ID.NS, entry.ID.Name)
			}
		}
	})

	t.Run("Copy", func(t *testing.T) {
		originalMap := NewStateMap(initialState)
		copiedMap := originalMap.Copy()

		if len(copiedMap) != len(originalMap) {
			t.Errorf("Copy() failed: expected map length %d, got %d", len(originalMap), len(copiedMap))
		}

		// Verify all entries are copied correctly
		for id, entry := range originalMap {
			if copiedEntry, exists := copiedMap[id]; !exists {
				t.Errorf("Copy() failed: entry with ID {ns: %s, name: %s} missing in copied map",
					id.NS, id.Name)
			} else if !reflect.DeepEqual(copiedEntry, entry) {
				t.Errorf("Copy() failed: entry mismatch for ID {ns: %s, name: %s}",
					id.NS, id.Name)
			}
		}

		// Verify copy is independent of original
		testID := entry1.ID
		delete(copiedMap, testID)
		if _, ok := originalMap[testID]; !ok {
			t.Error("Copy() failed: modifying copy affected original map")
		}
	})

	t.Run("ToSlice", func(t *testing.T) {
		stateMap := NewStateMap(initialState)
		newState := stateMap.ToSlice()

		if len(newState) != len(initialState) {
			t.Errorf("ToSlice() failed: expected slice length %d, got %d", len(initialState), len(newState))
		}

		// Create maps to compare entries by ID, since slice order isn't guaranteed
		originalEntries := make(map[registry.ID]bool)
		for _, entry := range initialState {
			originalEntries[entry.ID] = true
		}

		newEntries := make(map[registry.ID]bool)
		for _, entry := range newState {
			newEntries[entry.ID] = true

			// Verify each entry in new state matches an original entry
			found := false
			for _, originalEntry := range initialState {
				if reflect.DeepEqual(entry, originalEntry) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("ToSlice() failed: unexpected entry with ID {ns: %s, name: %s} in result",
					entry.ID.NS, entry.ID.Name)
			}
		}

		// Verify all original entries are present
		for id := range originalEntries {
			if !newEntries[id] {
				t.Errorf("ToSlice() failed: missing entry with ID {ns: %s, name: %s} in result",
					id.NS, id.Name)
			}
		}
	})

	t.Run("Empty State Operations", func(t *testing.T) {
		emptyState := registry.State{}

		// Test NewStateMap with empty state
		emptyMap := NewStateMap(emptyState)
		if len(emptyMap) != 0 {
			t.Errorf("NewStateMap() failed for empty state: expected length 0, got %d", len(emptyMap))
		}

		// Test Copy with empty map
		copiedEmpty := emptyMap.Copy()
		if len(copiedEmpty) != 0 {
			t.Errorf("Copy() failed for empty map: expected length 0, got %d", len(copiedEmpty))
		}

		// Test ToSlice with empty map
		emptySlice := emptyMap.ToSlice()
		if len(emptySlice) != 0 {
			t.Errorf("ToSlice() failed for empty map: expected length 0, got %d", len(emptySlice))
		}
	})
}
