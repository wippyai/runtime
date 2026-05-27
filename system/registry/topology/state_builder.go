// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"reflect"
	"sort"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
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
	log      *zap.Logger
	compare  func(a, b registry.Entry) bool
	resolver registry.DependencyResolver
}

// NewStateBuilder creates a new StateBuilder instance with the provided logger
func NewStateBuilder(log *zap.Logger, resolver registry.DependencyResolver, opt ...BuilderOption) *StateBuilder {
	sb := &StateBuilder{
		log:      log,
		resolver: resolver,
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
	case registry.EntryCreate:
		if _, exists := state[op.Entry.ID]; exists {
			return NewEntryExistsError(op.Entry.ID.NS, op.Entry.ID.Name)
		}

	case registry.EntryUpdate:
		existingEntry, exists := state[op.Entry.ID]
		if !exists {
			return NewEntryNotExistsError(op.Entry.ID.NS, op.Entry.ID.Name)
		}
		// Prevent kind changes during update
		if existingEntry.Kind != op.Entry.Kind {
			return NewKindChangeError(op.Entry.ID.NS, op.Entry.ID.Name, existingEntry.Kind, op.Entry.Kind)
		}

	case registry.EntryDelete:
		if _, exists := state[op.Entry.ID]; !exists {
			return NewDeleteNonExistentError(op.Entry.ID.NS, op.Entry.ID.Name)
		}
	default:
		return NewUnknownOperationKindError(op.Kind)
	}

	return nil
}

// ApplyOperation applies a single operation to the state and returns the new state
func (b *StateBuilder) ApplyOperation(state StateMap, op registry.Operation) (StateMap, error) {
	if err := b.ValidateOperation(state, op); err != nil {
		return state, NewInvalidOperationError(err)
	}

	newState := CopyStateMap(state)

	switch op.Kind {
	case registry.EntryCreate:
		newState[op.Entry.ID] = op.Entry
	case registry.EntryUpdate:
		newState[op.Entry.ID] = op.Entry
	case registry.EntryDelete:
		if _, ok := newState[op.Entry.ID]; ok {
			delete(newState, op.Entry.ID)
		} else {
			b.log.Warn("Attempted to delete non-existent entry",
				zap.String("namespace", op.Entry.ID.NS),
				zap.String("name", op.Entry.ID.Name))
		}
	default:
		return nil, NewUnknownOperationKindError(op.Kind)
	}

	return newState, nil
}

// GetInverseOperation returns the inverse of the given operation using OriginalEntry
func (b *StateBuilder) GetInverseOperation(op registry.Operation) (registry.Operation, error) {
	switch op.Kind {
	case registry.EntryCreate:
		return registry.Operation{Kind: registry.EntryDelete, Entry: op.Entry}, nil

	case registry.EntryUpdate:
		if op.OriginalEntry == nil {
			b.log.Warn("OriginalEntry not found for update operation, cannot create inverse",
				zap.String("namespace", op.Entry.ID.NS),
				zap.String("name", op.Entry.ID.Name))
			return registry.Operation{}, NewOriginalEntryNotFoundError(op.Entry.ID.NS, op.Entry.ID.Name)
		}
		return registry.Operation{Kind: registry.EntryUpdate, Entry: *op.OriginalEntry}, nil

	case registry.EntryDelete:
		if op.OriginalEntry == nil {
			b.log.Warn("OriginalEntry not found for delete operation, cannot create inverse",
				zap.String("namespace", op.Entry.ID.NS),
				zap.String("name", op.Entry.ID.Name))
			return registry.Operation{}, NewOriginalEntryNotFoundError(op.Entry.ID.NS, op.Entry.ID.Name)
		}
		return registry.Operation{Kind: registry.EntryCreate, Entry: *op.OriginalEntry}, nil

	default:
		return registry.Operation{}, NewUnknownOperationKindError(op.Kind)
	}
}

// BuildState constructs a registry State by applying the version history up to targetVersion.
func (b *StateBuilder) BuildState(history registry.History, targetVersion registry.Version) (registry.State, error) {
	vm := version.NewVersionMap()
	versions, err := history.Versions()
	if err != nil {
		return nil, NewGetVersionsError(err)
	}

	b.log.Debug("building state", zap.Uint("target_version", targetVersion.ID()), zap.Int("total_versions", len(versions)))

	var first registry.Version

	for _, v := range versions {
		if first == nil || first.ID() > v.ID() {
			first = v
		}

		err := vm.Add(v)
		if err != nil {
			b.log.Error("failed to add version to version map",
				zap.String("version", v.String()),
				zap.Error(err),
			)
		} else {
			b.log.Debug("added version to map", zap.Uint("version_id", v.ID()))
		}
	}

	if first == nil {
		return nil, NewNoVersionsFoundError()
	}

	b.log.Debug("computing path", zap.Uint("from", first.ID()), zap.Uint("to", targetVersion.ID()))

	path, err := vm.Path(first, targetVersion)
	if err != nil {
		b.log.Error("failed to compute version path",
			zap.Uint("from", first.ID()),
			zap.Uint("to", targetVersion.ID()),
			zap.Int("version_map_size", vm.Len()),
			zap.Error(err))
		return nil, NewComputePathError(targetVersion.String(), err)
	}

	b.log.Debug("path computed", zap.Int("path_length", len(path)))

	state := make(StateMap)

	// If path is empty but first == target, we still need to apply the first version's changeset
	// because Path excludes the source version
	if len(path) == 0 && first.ID() == targetVersion.ID() {
		changeSet, err := history.Get(first)
		if err != nil {
			return nil, NewGetChangesetError(first.String(), err)
		}
		for _, operation := range changeSet {
			newState, err := b.ApplyOperation(state, operation)
			if err != nil {
				return nil, NewApplyOperationError(first.String(), operation.Entry.ID.String(), err)
			}
			state = newState
		}
	}

	for _, ver := range path {
		b.log.Debug("building version transition", zap.String("version", ver.String()))

		changeSet, err := history.Get(ver)
		if err != nil {
			return nil, NewGetChangesetError(ver.String(), err)
		}

		for _, operation := range changeSet {
			newState, err := b.ApplyOperation(state, operation)
			if err != nil {
				return nil, NewApplyOperationError(ver.String(), operation.Entry.ID.String(), err)
			}
			state = newState
		}
	}

	return StateMapToSlice(state), nil
}

// SquashChangesets aggregates multiple changesets into a single changeset,
// combining operations on the same entry ID to minimize redundant operations.
// For example, if an entry is updated 10 times across versions, only the final update is kept.
func (b *StateBuilder) SquashChangesets(changesets []registry.ChangeSet) registry.ChangeSet {
	// Track the last operation for each entry ID
	operations := make(map[registry.ID]registry.Operation)

	// Process all changesets in order
	for _, changeset := range changesets {
		for _, op := range changeset {
			existing, exists := operations[op.Entry.ID]

			if !exists {
				// First operation for this entry
				operations[op.Entry.ID] = op
				continue
			}

			// Apply squashing rules based on the combination of operations
			switch existing.Kind {
			case registry.EntryCreate:
				switch op.Kind {
				case registry.EntryUpdate:
					// Create + Update = Create with updated value
					operations[op.Entry.ID] = registry.Operation{
						Kind:  registry.EntryCreate,
						Entry: op.Entry,
					}
				case registry.EntryDelete:
					// Create + Delete = Nothing (cancel out)
					delete(operations, op.Entry.ID)
				case registry.EntryCreate:
					// Create + Create = error in theory, but keep latest
					b.log.Warn("duplicate create operations for same entry",
						zap.String("id", op.Entry.ID.String()))
					operations[op.Entry.ID] = op
				}

			case registry.EntryUpdate:
				switch op.Kind {
				case registry.EntryUpdate:
					// Update + Update = Update with latest value
					operations[op.Entry.ID] = op
				case registry.EntryDelete:
					// Update + Delete = Delete
					operations[op.Entry.ID] = op
				case registry.EntryCreate:
					// Update + Create = shouldn't happen, but keep create
					b.log.Warn("create after update for same entry",
						zap.String("id", op.Entry.ID.String()))
					operations[op.Entry.ID] = op
				}

			case registry.EntryDelete:
				switch op.Kind {
				case registry.EntryCreate:
					// Delete + Create = Update (or Create if different kind)
					if existing.Entry.Kind == op.Entry.Kind {
						// Same kind, treat as update
						operations[op.Entry.ID] = registry.Operation{
							Kind:  registry.EntryUpdate,
							Entry: op.Entry,
						}
					} else {
						// Different kind, keep as create
						operations[op.Entry.ID] = op
					}
				case registry.EntryUpdate:
					// Delete + Update = shouldn't happen
					b.log.Error("update after delete for same entry",
						zap.String("id", op.Entry.ID.String()))
					// Keep the update but change to create
					operations[op.Entry.ID] = registry.Operation{
						Kind:  registry.EntryCreate,
						Entry: op.Entry,
					}
				case registry.EntryDelete:
					// Delete + Delete = keep delete
					b.log.Warn("duplicate delete operations for same entry",
						zap.String("id", op.Entry.ID.String()))
				}
			}
		}
	}

	// Convert map to slice in a deterministic order. Pre-fix this loop iterated
	// the operations map directly, so the resulting slice's element order
	// depended on the Go map hash seed. SortChangeSet below uses element
	// indexes to break ties between operations that have no dependency
	// relationship, so a randomized input produced a randomized output even
	// after topological sorting.
	result := make(registry.ChangeSet, 0, len(operations))
	for _, op := range operations {
		result = append(result, op)
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i].Entry.ID, result[j].Entry.ID
		if a.NS != b.NS {
			return a.NS < b.NS
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return result[i].Kind < result[j].Kind
	})

	// If no operations, return empty
	if len(result) == 0 {
		return result
	}

	// Sort with operation semantics. Delete operations must run in reverse
	// dependency order (dependents before dependencies), while creates and
	// updates run dependency-first.
	fromState := make(registry.State, 0, len(result))
	for _, op := range result {
		fromState = append(fromState, op.Entry)
	}
	sorted, err := b.SortChangeSet(fromState, result)
	if err != nil {
		// Log error but still return the operations unsorted
		// This ensures operations are applied even if dependency sorting fails
		b.log.Error("failed to sort squashed operations by dependency",
			zap.Int("operation_count", len(operations)),
			zap.Error(err))
		return result
	}

	return sorted
}

// ReverseChangeset creates a changeset that undoes the given changeset operations.
func (b *StateBuilder) ReverseChangeset(changeset registry.ChangeSet) (registry.ChangeSet, error) {
	reversed := make(registry.ChangeSet, 0, len(changeset))

	// Process in reverse order to maintain dependency relationships
	for i := len(changeset) - 1; i >= 0; i-- {
		op := changeset[i]
		inverseOp, err := b.GetInverseOperation(op)
		if err != nil {
			b.log.Warn("failed to reverse operation",
				zap.String("kind", op.Kind),
				zap.String("entry", op.Entry.ID.String()),
				zap.Error(err))
			continue
		}
		reversed = append(reversed, inverseOp)
	}

	return reversed, nil
}

// BuildDelta calculates the changes required to transition from one state to another.
func (b *StateBuilder) BuildDelta(from, to registry.State) (registry.ChangeSet, error) {
	fromState := NewStateMap(from)
	toState := NewStateMap(to)

	operations := make(registry.ChangeSet, 0, len(from)+len(to))

	// Find deletes (entries in 'from' but not in 'to')
	for _, fromEntry := range from {
		if _, exists := toState[fromEntry.ID]; !exists {
			operations = append(operations, registry.Operation{
				Kind:  registry.EntryDelete,
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
				Kind:  registry.EntryCreate,
				Entry: toEntry,
			})
		} else if !b.compare(fromEntry, toEntry) {
			// Update
			operations = append(operations, registry.Operation{
				Kind:  registry.EntryUpdate,
				Entry: toEntry,
			})
		}
	}

	return b.SortChangeSet(from, operations)
}
