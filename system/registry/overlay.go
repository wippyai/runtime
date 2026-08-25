// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/registry/topology"
)

// ApplyOverlay applies an owner-scoped process-local changeset without
// advancing registry history. The entries remain part of effective state until
// explicitly cleared or the process exits.
func (r *Reg) ApplyOverlay(ctx context.Context, owner string, expectedGeneration uint64, changes registry.ChangeSet) (uint64, error) {
	r.applyMu.Lock()
	defer r.applyMu.Unlock()
	return r.applyOverlayLocked(ctx, owner, expectedGeneration, changes)
}

// GetOverlay returns a copy of one owner's live entries and generation.
func (r *Reg) GetOverlay(owner string) (registry.State, uint64, error) {
	var err error
	owner, err = registry.CanonicalOverlayOwner(owner)
	if err != nil {
		return nil, 0, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	generation, knownOwner := r.overlayGeneration[owner]
	if !knownOwner {
		generation = r.overlayFloor
	}
	return cloneOverlayState(r.overlays[owner]), generation, nil
}

func (r *Reg) applyOverlayLocked(ctx context.Context, owner string, expectedGeneration uint64, changes registry.ChangeSet) (uint64, error) {
	var err error
	owner, err = registry.CanonicalOverlayOwner(owner)
	if err != nil {
		return 0, err
	}
	if len(changes) == 0 {
		return 0, NewOverlayValidationError("registry overlay changes are required", nil)
	}
	changes = append(registry.ChangeSet(nil), changes...)
	canonicalizeChangeSetIDs(changes)

	r.mu.RLock()
	snapshot := append(registry.State(nil), r.state...)
	liveProv := r.provenanceSnapshot()
	currentGeneration, activeOwner := r.overlayGeneration[owner]
	if !activeOwner {
		currentGeneration = r.overlayFloor
	}
	owners := make(map[registry.ID]string, len(r.overlayOwners))
	for id, value := range r.overlayOwners {
		owners[id] = value
	}
	r.mu.RUnlock()
	if currentGeneration != expectedGeneration {
		return 0, NewOverlayGenerationConflictError(owner, expectedGeneration, currentGeneration)
	}

	effective := topology.NewStateMap(snapshot)
	candidateOwners := make(map[registry.ID]string, len(owners)+len(changes))
	for id, entryOwner := range owners {
		candidateOwners[id] = entryOwner
	}
	seen := make(map[registry.ID]struct{}, len(changes))
	deleted := make(map[registry.ID]struct{})
	for i := range changes {
		op := &changes[i]
		op.Entry = cloneOverlayEntry(op.Entry)
		if op.OriginalEntry != nil {
			original := cloneOverlayEntry(*op.OriginalEntry)
			op.OriginalEntry = &original
		}
		if _, duplicate := seen[op.Entry.ID]; duplicate {
			return 0, NewOverlayValidationError("registry overlay changes contain a duplicate entry", map[string]any{"entry_id": op.Entry.ID.String()})
		}
		seen[op.Entry.ID] = struct{}{}
		entryOwner, overlayEntry := candidateOwners[op.Entry.ID]
		switch op.Kind {
		case registry.EntryCreate:
			if overlayEntry {
				return 0, NewOverlayConflictError("registry overlay entry is already owned", map[string]any{"entry_id": op.Entry.ID.String(), "owner": entryOwner})
			}
			if _, exists := effective[op.Entry.ID]; exists {
				return 0, NewOverlayConflictError("registry overlay entry conflicts with durable state", map[string]any{"entry_id": op.Entry.ID.String()})
			}
		case registry.EntryUpdate, registry.EntryDelete:
			if !overlayEntry || entryOwner != owner {
				return 0, NewOverlayConflictError("registry overlay owner cannot mutate entry", map[string]any{"entry_id": op.Entry.ID.String(), "owner": owner})
			}
		default:
			return 0, NewOverlayValidationError("unknown registry overlay operation", map[string]any{"operation": op.Kind})
		}
		if op.Kind != registry.EntryDelete {
			if err := validateOverlayEntryProvenance(op.Entry); err != nil {
				return 0, err
			}
			if len(r.directivesByKind[op.Entry.Kind]) != 0 {
				return 0, NewOverlayValidationError("registry overlay entries cannot use directive-owned kinds", map[string]any{
					"entry_id": op.Entry.ID.String(),
					"kind":     op.Entry.Kind,
				})
			}
		}
		switch op.Kind {
		case registry.EntryCreate, registry.EntryUpdate:
			effective[op.Entry.ID] = op.Entry
			candidateOwners[op.Entry.ID] = owner
		case registry.EntryDelete:
			delete(effective, op.Entry.ID)
			delete(candidateOwners, op.Entry.ID)
			deleted[op.Entry.ID] = struct{}{}
		}
	}
	if len(deleted) != 0 {
		if err := r.validateRemovedOverlayDependencies(topology.NewStateMap(snapshot), effective, deleted); err != nil {
			return 0, err
		}
	}
	if err := r.validateOverlayComposition(effective, candidateOwners); err != nil {
		return 0, err
	}

	sorted, err := r.sortWithIndex(snapshot, changes)
	if err != nil {
		return 0, NewSortChangesError(err)
	}
	nextProv, err := applyOpsToProvenance(liveProv, sorted)
	if err != nil {
		return 0, NewProvenanceInvariantError(err)
	}
	annotateChangeSet(sorted, liveProv, nextProv)

	r.mu.Lock()
	defer r.mu.Unlock()
	newState, err := r.runner.Transition(ctx, r.state, sorted)
	if err != nil {
		if newState != nil && ctx.Err() == nil {
			if rollbackErr := r.rollback(ctx, newState, r.state, nextProv, nil); rollbackErr != nil {
				r.reconcileOverlayIndexesAfterFailedRollback(owner, owners, changes)
				return 0, NewApplyChangesError(err, rollbackErr)
			}
		}
		return 0, NewApplyChangesError(err, nil)
	}

	r.rebuildOverlayIndexes(candidateOwners, newState)
	nextGeneration := r.bumpOverlayGeneration(owner)
	r.state = newState
	r.rebuildIndex()
	r.patchDepIndex(sorted)
	r.publishProvenance(nextProv)
	return nextGeneration, nil
}

// bumpOverlayGeneration issues process-unique generation tokens and retains a
// tombstone only for owners that mutated. This prevents ABA after a complete
// delete without making unrelated owners conflict or growing state on reads.
func (r *Reg) bumpOverlayGeneration(owner string) uint64 {
	r.overlayEpoch++
	r.overlayGeneration[owner] = r.overlayEpoch
	return r.overlayEpoch
}

func (r *Reg) validateRemovedOverlayDependencies(before, after registry.StateMap, deleted map[registry.ID]struct{}) error {
	if len(deleted) == 0 {
		return nil
	}
	return topology.VisitDependencies(before, r.resolver, func(source, target registry.ID) error {
		if _, survives := after[source]; !survives {
			return nil
		}
		if _, removed := deleted[target]; removed {
			return NewOverlayConflictError("registry overlay deletion removes a live dependency", map[string]any{
				"dependency_id": target.String(),
				"entry_id":      source.String(),
			})
		}
		return nil
	})
}

func cloneOverlayState(state registry.State) registry.State {
	if len(state) == 0 {
		return nil
	}
	cloned := make(registry.State, len(state))
	for i, entry := range state {
		cloned[i] = cloneOverlayEntry(entry)
	}
	return cloned
}

func cloneOverlayEntry(entry registry.Entry) registry.Entry {
	meta := attrs.NewBag()
	for key, value := range entry.Meta {
		meta[key] = payload.SnapshotData(value)
	}
	entry.Meta = meta
	entry.Data = payload.Snapshot(entry.Data)
	return entry
}

func validateOverlayEntryProvenance(entry registry.Entry) error {
	if entry.DependencyRoot {
		return NewOverlayValidationError("registry overlay entry cannot be a deployment root", map[string]any{"entry_id": entry.ID.String()})
	}
	return nil
}

func (r *Reg) validateOverlayComposition(effective registry.StateMap, owners map[registry.ID]string) error {
	if len(owners) == 0 {
		return nil
	}
	return topology.VisitDependencies(effective, r.resolver, func(source, target registry.ID) error {
		sourceOwner, sourceOverlay := owners[source]
		targetOwner, targetOverlay := owners[target]
		switch {
		case sourceOverlay && targetOverlay && sourceOwner != targetOwner:
			return NewOverlayConflictError("registry overlay dependency crosses owner boundary", map[string]any{
				"entry_id":      source.String(),
				"owner":         sourceOwner,
				"dependency_id": target.String(),
				"target_owner":  targetOwner,
			})
		case !sourceOverlay && targetOverlay:
			return NewOverlayConflictError("durable registry entry depends on process-local overlay", map[string]any{
				"entry_id":      source.String(),
				"dependency_id": target.String(),
				"owner":         targetOwner,
			})
		}
		return nil
	})
}

// mergeOverlayProvenance carries process-local entries across durable version
// selection. Overlay ownership remains in overlayOwners; its provenance record
// is the explicit host record used by the total live-state map.
func (r *Reg) mergeOverlayProvenance(target, live registry.ProvenanceMap) error {
	for id := range r.overlayOwners {
		p, ok := live[id]
		if !ok {
			return registry.NewMissingProvenanceError(id)
		}
		target[id] = p
	}
	return nil
}

func (r *Reg) validateDurableTransitionAgainstOverlays(allOps registry.ChangeSet) error {
	if len(r.overlayOwners) == 0 {
		return nil
	}
	for _, op := range allOps {
		id := canonicalEntryID(op.Entry.ID)
		if owner, ok := r.overlayOwners[id]; ok {
			return NewOverlayConflictError("durable transition targets a process-local overlay entry", map[string]any{
				"entry_id": op.Entry.ID.String(),
				"owner":    owner,
			})
		}
	}
	current := topology.NewStateMap(r.state)
	deleted := make(map[registry.ID]struct{})
	for _, op := range allOps {
		if op.Kind == registry.EntryDelete {
			deleted[canonicalEntryID(op.Entry.ID)] = struct{}{}
		}
	}
	if err := topology.VisitDependencies(current, r.resolver, func(source, target registry.ID) error {
		owner, overlay := r.overlayOwners[source]
		if _, removed := deleted[target]; overlay && removed {
			return NewOverlayConflictError("durable transition removes an overlay dependency", map[string]any{
				"dependency_id": target.String(),
				"entry_id":      source.String(),
				"owner":         owner,
			})
		}
		return nil
	}); err != nil {
		return err
	}
	applyStateOperations(current, allOps)
	return r.validateOverlayComposition(current, r.overlayOwners)
}

func (r *Reg) composeOverlays(stateMap registry.StateMap) error {
	for id, entry := range stateMap {
		canonicalID := canonicalEntryID(id)
		entry.ID = canonicalEntryID(entry.ID)
		if !canonicalID.Equal(entry.ID) {
			return fmt.Errorf("durable state key %s does not match entry %s", id, entry.ID)
		}
		if canonicalID != id {
			delete(stateMap, id)
			if _, duplicate := stateMap[canonicalID]; duplicate {
				return fmt.Errorf("selected durable version contains duplicate entry %s", canonicalID)
			}
			stateMap[canonicalID] = entry
		}
	}
	for owner, entries := range r.overlays {
		for _, entry := range entries {
			entry.ID = canonicalEntryID(entry.ID)
			if _, exists := stateMap[entry.ID]; exists {
				return fmt.Errorf("overlay entry %s owned by %s conflicts with selected durable version", entry.ID, owner)
			}
			stateMap[entry.ID] = entry
		}
	}
	return r.validateOverlayComposition(stateMap, r.overlayOwners)
}

func (r *Reg) reconcileOverlayIndexesAfterFailedRollback(owner string, previousOwners map[registry.ID]string, changes registry.ChangeSet) {
	knownOwners := make(map[registry.ID]string, len(previousOwners)+len(changes))
	ownerGenerationInvalidated := false
	for id, entryOwner := range previousOwners {
		knownOwners[id] = entryOwner
		if entryOwner == owner {
			ownerGenerationInvalidated = true
		}
	}
	for _, op := range changes {
		if op.Kind != registry.EntryDelete {
			knownOwners[canonicalEntryID(op.Entry.ID)] = owner
		}
	}

	r.rebuildOverlayIndexes(knownOwners, r.state)
	if !ownerGenerationInvalidated {
		r.bumpOverlayGeneration(owner)
	}
}

func (r *Reg) reconcileKnownOverlaysAfterFailedRollback() {
	owners := make(map[string]struct{})
	knownOwners := make(map[registry.ID]string, len(r.overlayOwners))
	for id, owner := range r.overlayOwners {
		knownOwners[id] = owner
		owners[owner] = struct{}{}
	}
	r.rebuildOverlayIndexes(knownOwners, r.state)
	for owner := range owners {
		r.bumpOverlayGeneration(owner)
	}
}

func (r *Reg) rebuildOverlayIndexes(knownOwners map[registry.ID]string, effective registry.State) {
	nextOwners := make(map[registry.ID]string)
	nextOverlays := make(map[string]registry.StateMap)
	for _, entry := range effective {
		id := entry.ID
		entryOwner, isOverlay := knownOwners[id]
		if !isOverlay {
			continue
		}
		entry.ID = id
		nextOwners[id] = entryOwner
		if nextOverlays[entryOwner] == nil {
			nextOverlays[entryOwner] = make(registry.StateMap)
		}
		nextOverlays[entryOwner][id] = cloneOverlayEntry(entry)
	}
	r.overlayOwners = nextOwners
	r.overlays = make(map[string]registry.State, len(nextOverlays))
	for entryOwner, entries := range nextOverlays {
		r.overlays[entryOwner] = topology.StateMapToSlice(entries)
	}
}

var _ registry.OverlayWriter = (*Reg)(nil)
