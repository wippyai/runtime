package topology

import (
	"reflect"

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
	case registry.Create:
		if _, exists := state[op.Entry.ID]; exists {
			return NewEntryExistsError(op.Entry.ID.NS, op.Entry.ID.Name)
		}

	case registry.Update:
		existingEntry, exists := state[op.Entry.ID]
		if !exists {
			return NewEntryNotExistsError(op.Entry.ID.NS, op.Entry.ID.Name)
		}
		// Prevent kind changes during update
		if existingEntry.Kind != op.Entry.Kind {
			return NewKindChangeError(op.Entry.ID.NS, op.Entry.ID.Name, existingEntry.Kind, op.Entry.Kind)
		}

	case registry.Delete:
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
		return nil, NewUnknownOperationKindError(op.Kind)
	}

	return newState, nil
}

// GetInverseOperation returns the inverse of the given operation using OriginalEntry
func (b *StateBuilder) GetInverseOperation(op registry.Operation) (registry.Operation, error) {
	switch op.Kind {
	case registry.Create:
		return registry.Operation{Kind: registry.Delete, Entry: op.Entry}, nil

	case registry.Update:
		if op.OriginalEntry == nil {
			b.log.Warn("OriginalEntry not found for update operation, cannot create inverse",
				zap.String("namespace", op.Entry.ID.NS),
				zap.String("name", op.Entry.ID.Name))
			return registry.Operation{}, NewOriginalEntryNotFoundError(op.Entry.ID.NS, op.Entry.ID.Name)
		}
		return registry.Operation{Kind: registry.Update, Entry: *op.OriginalEntry}, nil

	case registry.Delete:
		if op.OriginalEntry == nil {
			b.log.Warn("OriginalEntry not found for delete operation, cannot create inverse",
				zap.String("namespace", op.Entry.ID.NS),
				zap.String("name", op.Entry.ID.Name))
			return registry.Operation{}, NewOriginalEntryNotFoundError(op.Entry.ID.NS, op.Entry.ID.Name)
		}
		return registry.Operation{Kind: registry.Create, Entry: *op.OriginalEntry}, nil

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

	for _, ver := range path {
		b.log.Debug("building version transition", zap.String("version", ver.String()))

		changeSet, err := history.Get(ver)
		if err != nil {
			return nil, NewGetChangesetError(ver.String(), err)
		}

		for _, operation := range changeSet {
			newState, err := b.ApplyOperation(state, operation)
			if err != nil {
				b.log.Error("failed to apply operation",
					zap.String("version", ver.String()),
					zap.Error(err))
				continue
			}
			state = newState
		}
	}

	return state.ToSlice(), nil
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
			case registry.Create:
				switch op.Kind {
				case registry.Update:
					// Create + Update = Create with updated value
					operations[op.Entry.ID] = registry.Operation{
						Kind:  registry.Create,
						Entry: op.Entry,
					}
				case registry.Delete:
					// Create + Delete = Nothing (cancel out)
					delete(operations, op.Entry.ID)
				case registry.Create:
					// Create + Create = error in theory, but keep latest
					b.log.Warn("duplicate create operations for same entry",
						zap.String("id", op.Entry.ID.String()))
					operations[op.Entry.ID] = op
				}

			case registry.Update:
				switch op.Kind {
				case registry.Update:
					// Update + Update = Update with latest value
					operations[op.Entry.ID] = op
				case registry.Delete:
					// Update + Delete = Delete
					operations[op.Entry.ID] = op
				case registry.Create:
					// Update + Create = shouldn't happen, but keep create
					b.log.Warn("create after update for same entry",
						zap.String("id", op.Entry.ID.String()))
					operations[op.Entry.ID] = op
				}

			case registry.Delete:
				switch op.Kind {
				case registry.Create:
					// Delete + Create = Update (or Create if different kind)
					if existing.Entry.Kind == op.Entry.Kind {
						// Same kind, treat as update
						operations[op.Entry.ID] = registry.Operation{
							Kind:  registry.Update,
							Entry: op.Entry,
						}
					} else {
						// Different kind, keep as create
						operations[op.Entry.ID] = op
					}
				case registry.Update:
					// Delete + Update = shouldn't happen
					b.log.Error("update after delete for same entry",
						zap.String("id", op.Entry.ID.String()))
					// Keep the update but change to create
					operations[op.Entry.ID] = registry.Operation{
						Kind:  registry.Create,
						Entry: op.Entry,
					}
				case registry.Delete:
					// Delete + Delete = keep delete
					b.log.Warn("duplicate delete operations for same entry",
						zap.String("id", op.Entry.ID.String()))
				}
			}
		}
	}

	// Convert map to slice and collect entries for sorting
	result := make(registry.ChangeSet, 0, len(operations))
	entries := make([]registry.Entry, 0, len(operations))

	for _, op := range operations {
		result = append(result, op)
		entries = append(entries, op.Entry)
	}

	// If no operations, return empty
	if len(result) == 0 {
		return result
	}

	// Sort by dependencies
	sortedEntries, err := SortEntriesByDependency(entries, b.resolver)
	if err != nil {
		// Log error but still return the operations unsorted
		// This ensures operations are applied even if dependency sorting fails
		b.log.Error("failed to sort squashed operations by dependency",
			zap.Int("operation_count", len(operations)),
			zap.Error(err))
		return result
	}

	// Build map for O(1) lookup
	opByID := make(map[registry.ID]registry.Operation, len(operations))
	for _, op := range result {
		opByID[op.Entry.ID] = op
	}

	// Rebuild changeset in dependency order using map lookup
	sorted := make(registry.ChangeSet, 0, len(operations))
	for _, entry := range sortedEntries {
		if op, ok := opByID[entry.ID]; ok {
			sorted = append(sorted, op)
		}
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
	sortedEntries, err := SortEntriesByDependency(opEntries, b.resolver)
	if err != nil {
		return nil, err
	}

	// Build map for O(1) lookup of operations by ID
	opByID := make(map[registry.ID]registry.Operation, len(operations))
	for _, op := range operations {
		opByID[op.Entry.ID] = op
	}

	// Map back to operations maintaining the sorted order
	result := make(registry.ChangeSet, 0, len(operations))
	processed := make(map[registry.ID]bool, len(operations))

	// First pass: handle deletes in reverse dependency order
	for i := len(sortedEntries) - 1; i >= 0; i-- {
		entry := sortedEntries[i]
		if op, ok := opByID[entry.ID]; ok && op.Kind == registry.Delete {
			result = append(result, op)
			processed[op.Entry.ID] = true
		}
	}

	// Second pass: handle updates and creates in dependency order
	for _, entry := range sortedEntries {
		if processed[entry.ID] {
			continue
		}
		if op, ok := opByID[entry.ID]; ok {
			result = append(result, op)
			processed[op.Entry.ID] = true
		}
	}

	return result, nil
}
