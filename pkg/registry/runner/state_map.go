package runner

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/ponyruntime/pony/api/registry"
)

// stateMap is an internal representation of the state using a map for faster lookups.
type stateMap map[registry.Path]registry.Entry

// stateHelper encapsulates state-related operations.
type stateHelper struct {
	log *zap.Logger
}

// newStateHelper creates a new stateHelper instance.
func newStateHelper(log *zap.Logger) *stateHelper {
	return &stateHelper{
		log: log,
	}
}

// toMap converts a State (slice) to a stateMap (map).
func (sh *stateHelper) toMap(state registry.State) stateMap {
	m := make(stateMap)
	for _, entry := range state {
		m[entry.Path] = entry
	}
	return m
}

// toSlice converts a stateMap (map) to a State (slice).
func (sh *stateHelper) toSlice(state stateMap) registry.State {
	slice := make(registry.State, 0, len(state))
	for _, entry := range state {
		slice = append(slice, entry)
	}
	return slice
}

// copy creates a shallow copy of the stateMap.
func (sh *stateHelper) copy(state stateMap) stateMap {
	newMap := make(stateMap)
	for k, v := range state {
		newMap[k] = v
	}
	return newMap
}

// applyChangeToState applies the change defined by the operation to the stateMap.
func (sh *stateHelper) applyChangeToState(state stateMap, op registry.Operation) (stateMap, error) {
	newState := sh.copy(state) // Copy the map

	switch op.Kind {
	case registry.Create:
		newState[op.Entry.Path] = op.Entry
	case registry.Update:
		newState[op.Entry.Path] = op.Entry
	case registry.Delete:
		if _, ok := newState[op.Entry.Path]; ok {
			delete(newState, op.Entry.Path)
		} else {
			sh.log.Warn("Attempted to delete non-existent entry", zap.String("path", string(op.Entry.Path)))
		}
	default:
		return nil, fmt.Errorf("unknown operation kind: %s", op.Kind)
	}

	return newState, nil
}

// getInverseOperation returns the inverse of the given operation, utilizing the original state for accuracy.
func (sh *stateHelper) getInverseOperation(state stateMap, op registry.Operation) (registry.Operation, error) {
	switch op.Kind {
	case registry.Create:
		return registry.Operation{Kind: registry.Delete, Entry: op.Entry}, nil // Delete is the inverse of Create.
	case registry.Update:
		// Fetch the original entry from the state to ensure we revert to the correct state.
		originalEntry, exists := state[op.Entry.Path]
		if !exists {
			// If the entry doesn't exist in the original state, we can't perform an update. Log a warning and skip.
			sh.log.Warn("Original entry not found for update operation, cannot create inverse", zap.String("path", string(op.Entry.Path)))
			return registry.Operation{}, fmt.Errorf("original entry not found for path: %s", op.Entry.Path)
		}
		return registry.Operation{Kind: registry.Update, Entry: originalEntry}, nil
	case registry.Delete:
		// For delete, the inverse is to create the entry as it was originally.
		originalEntry, exists := state[op.Entry.Path]
		if !exists {
			// If the entry doesn't exist in the original state, we can't recreate it. Log a warning and skip.
			sh.log.Warn("Original entry not found for delete operation, cannot create inverse", zap.String("path", string(op.Entry.Path)))
			return registry.Operation{}, fmt.Errorf("original entry not found for path: %s", op.Entry.Path)
		}
		return registry.Operation{Kind: registry.Create, Entry: originalEntry}, nil
	default:
		return registry.Operation{}, fmt.Errorf("unknown operation kind: %s", op.Kind)
	}
}
