package topology

import (
	"fmt"
	"reflect"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/version"
	"go.uber.org/zap"
)

type BuilderOption func(*StateBuilder)

// WithCompareFunc sets the comparison function for entries
func WithCompareFunc(compare func(a, b registry.Entry) bool) BuilderOption {
	return func(b *StateBuilder) {
		b.compare = compare
	}
}

// StateBuilder constructs registry states and calculates state transitions
type StateBuilder struct {
	log     *zap.Logger
	compare func(a, b registry.Entry) bool
}

// NewStateBuilder creates a new StateBuilder instance with the provided logger
func NewStateBuilder(log *zap.Logger, opt ...BuilderOption) *StateBuilder {
	sb := &StateBuilder{
		log: log,
		compare: func(a, b registry.Entry) bool {
			return reflect.DeepEqual(a, b)
		},
	}

	for _, o := range opt {
		o(sb)
	}

	return sb
}

// ValidateOperation validates if an operation can be applied to the current state
func (b *StateBuilder) ValidateOperation(state StateMap, op registry.Operation) error {
	switch op.Kind {
	case registry.Create:
		if _, exists := state[op.Entry.ID]; exists {
			return fmt.Errorf("entry already exists: {ns: %s, name: %s}",
				op.Entry.ID.NS, op.Entry.ID.Name)
		}

	case registry.Update:
		existingEntry, exists := state[op.Entry.ID]
		if !exists {
			return fmt.Errorf("entry does not exist: {ns: %s, name: %s}",
				op.Entry.ID.NS, op.Entry.ID.Name)
		}
		// Prevent kind changes during update
		if existingEntry.Kind != op.Entry.Kind {
			return fmt.Errorf("cannot change entry kind from %s to %s for {ns: %s, name: %s}",
				existingEntry.Kind, op.Entry.Kind, op.Entry.ID.NS, op.Entry.ID.Name)
		}

	case registry.Delete:
		if _, exists := state[op.Entry.ID]; !exists {
			return fmt.Errorf("cannot delete non-existent entry: {ns: %s, name: %s}",
				op.Entry.ID.NS, op.Entry.ID.Name)
		}
	default:
		return fmt.Errorf("unknown operation kind: %s", op.Kind)
	}

	return nil
}

// ApplyOperation applies a single operation to the state and returns the new state
func (b *StateBuilder) ApplyOperation(state StateMap, op registry.Operation) (StateMap, error) {
	if err := b.ValidateOperation(state, op); err != nil {
		return state, fmt.Errorf("invalid operation: %w", err)
	}

	newState := state.Copy() // Spawn a copy of the state

	switch op.Kind {
	case registry.Create:
		newState[op.Entry.ID] = op.Entry
	case registry.Update:
		newState[op.Entry.ID] = op.Entry
	case registry.Delete:
		if _, ok := newState[op.Entry.ID]; ok {
			delete(newState, op.Entry.ID)
		} else {
			b.log.Warn("Attempted to delete non-existent entry",
				zap.String("namespace", op.Entry.ID.NS),
				zap.String("name", op.Entry.ID.Name))
		}
	default:
		return nil, fmt.Errorf("unknown operation kind: %s", op.Kind)
	}

	return newState, nil
}

// GetInverseOperation returns the inverse of the given operation
func (b *StateBuilder) GetInverseOperation(state StateMap, op registry.Operation) (registry.Operation, error) {
	switch op.Kind {
	case registry.Create:
		return registry.Operation{Kind: registry.Delete, Entry: op.Entry}, nil

	case registry.Update:
		originalEntry, exists := state[op.Entry.ID]
		if !exists {
			b.log.Warn("Original entry not found for update operation, cannot create inverse",
				zap.String("namespace", op.Entry.ID.NS),
				zap.String("name", op.Entry.ID.Name))
			return registry.Operation{}, fmt.Errorf("original entry not found for Process {ns: %s, name: %s}",
				op.Entry.ID.NS, op.Entry.ID.Name)
		}
		return registry.Operation{Kind: registry.Update, Entry: originalEntry}, nil

	case registry.Delete:
		originalEntry, exists := state[op.Entry.ID]
		if !exists {
			b.log.Warn("Original entry not found for delete operation, cannot create inverse",
				zap.String("namespace", op.Entry.ID.NS),
				zap.String("name", op.Entry.ID.Name))
			return registry.Operation{}, fmt.Errorf("original entry not found for Process {ns: %s, name: %s}",
				op.Entry.ID.NS, op.Entry.ID.Name)
		}
		return registry.Operation{Kind: registry.Create, Entry: originalEntry}, nil

	default:
		return registry.Operation{}, fmt.Errorf("unknown operation kind: %s", op.Kind)
	}
}

// BuildState constructs a registry State by applying the version history up to targetVersion.
func (b *StateBuilder) BuildState(history registry.History, targetVersion registry.Version) (registry.State, error) {
	vm := version.NewVersionMap()
	versions, err := history.Versions()
	if err != nil {
		return nil, fmt.Errorf("get versions from history: %w", err)
	}

	var first registry.Version

	for _, v := range versions {
		if first == nil || first.ID() > v.ID() {
			first = v
		}

		err := vm.Add(v)
		if err != nil {
			b.log.Error("add version to version map",
				zap.String("version", v.String()),
				zap.Error(err),
			)
		}
	}

	if first == nil {
		return nil, fmt.Errorf("no versions found in history")
	}

	path, err := vm.Path(first, targetVersion)
	if err != nil {
		return nil, fmt.Errorf("get path from root to version %v: %w", targetVersion, err)
	}

	state := make(StateMap)

	for _, ver := range path {
		b.log.Debug("building version transition", zap.String("version", ver.String()))

		changeSet, err := history.Get(ver)
		if err != nil {
			return nil, fmt.Errorf("get changeset for version %v: %w", ver, err)
		}

		for _, operation := range changeSet {
			newState, err := b.ApplyOperation(state, operation)
			if err != nil {
				b.log.Error("apply operation",
					zap.String("version", ver.String()),
					zap.Error(err))
				continue
			}
			state = newState
		}
	}

	return state.ToSlice(), nil
}

// BuildDelta calculates the changes required to transition from one state to another.
func (b *StateBuilder) BuildDelta(from, to registry.State) (registry.ChangeSet, error) {
	fromState := NewStateMap(from)
	toState := NewStateMap(to)

	var operations []registry.Operation

	// Find deletes (entries in 'from' but not in 'to')
	for _, fromEntry := range from {
		if _, exists := toState[fromEntry.ID]; !exists {
			operations = append(operations, registry.Operation{
				Kind:  registry.Delete,
				Entry: fromEntry,
			})
		}
	}

	// Find creates and updates
	for _, toEntry := range to {
		fromEntry, exists := fromState[toEntry.ID]
		if !exists {
			// Spawn
			operations = append(operations, registry.Operation{
				Kind:  registry.Create,
				Entry: toEntry,
			})
		} else if !b.compare(fromEntry, toEntry) {
			// Update
			operations = append(operations, registry.Operation{
				Kind:  registry.Update,
				Entry: toEntry,
			})
		}
	}

	// Build dependency relationships for all operations
	opEntries := make([]registry.Entry, 0, len(operations))
	for _, op := range operations {
		// For deletes, we need to invert dependencies
		if op.Kind == registry.Delete {
			invertedEntry := op.Entry
			// Get all entries that depend on this one and make this entry depend on them
			var dependedOnBy []string
			for _, entry := range to {
				if dependsOn, ok := entry.Meta[registry.TagDependsOn].([]string); ok {
					for _, dep := range dependsOn {
						if resolveDependencyID(entry.ID.NS, dep) == op.Entry.ID {
							dependedOnBy = append(dependedOnBy, entry.ID.String())
						}
					}
				}
			}
			if len(dependedOnBy) > 0 {
				invertedEntry.Meta = make(map[string]any)
				for k, v := range op.Entry.Meta {
					invertedEntry.Meta[k] = v
				}
				invertedEntry.Meta[registry.TagDependsOn] = dependedOnBy
			}
			opEntries = append(opEntries, invertedEntry)
		} else {
			opEntries = append(opEntries, op.Entry)
		}
	}

	// Sort entries respecting dependencies
	sortedEntries, err := SortEntriesByDependency(opEntries)
	if err != nil {
		return nil, err
	}

	// Map back to operations maintaining the sorted order
	result := make(registry.ChangeSet, 0, len(operations))
	processed := make(map[registry.ID]bool)

	// First pass: handle deletes in reverse dependency order
	for i := len(sortedEntries) - 1; i >= 0; i-- {
		entry := sortedEntries[i]
		for _, op := range operations {
			if op.Kind == registry.Delete && op.Entry.ID == entry.ID {
				result = append(result, op)
				processed[op.Entry.ID] = true
				break
			}
		}
	}

	// Second pass: handle updates and creates in dependency order
	for _, entry := range sortedEntries {
		if processed[entry.ID] {
			continue
		}
		for _, op := range operations {
			if op.Entry.ID == entry.ID {
				result = append(result, op)
				processed[op.Entry.ID] = true
				break
			}
		}
	}

	return result, nil
}
