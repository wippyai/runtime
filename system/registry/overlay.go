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

const overlayOwnerMeta = "overlay_owner"

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
	state, ok := r.overlays[owner]
	if !ok {
		return nil, r.overlayEpoch, nil
	}
	return cloneOverlayState(state), r.overlayGeneration[owner], nil
}

func (r *Reg) applyOverlayLocked(ctx context.Context, owner string, expectedGeneration uint64, changes registry.ChangeSet) (uint64, error) {
	var err error
	owner, err = registry.CanonicalOverlayOwner(owner)
	if err != nil {
		return 0, err
	}
	if len(changes) == 0 {
		return 0, fmt.Errorf("registry overlay changes are required")
	}
	changes = append(registry.ChangeSet(nil), changes...)
	canonicalizeChangeSetIDs(changes)

	r.mu.RLock()
	snapshot := append(registry.State(nil), r.state...)
	currentGeneration, activeOwner := r.overlayGeneration[owner]
	if !activeOwner {
		currentGeneration = r.overlayEpoch
	}
	owners := make(map[registry.ID]string, len(r.overlayOwners))
	for id, value := range r.overlayOwners {
		owners[id] = value
	}
	r.mu.RUnlock()
	if currentGeneration != expectedGeneration {
		return 0, NewOverlayGenerationConflictError(owner, expectedGeneration, currentGeneration)
	}

	effective := canonicalStateMap(snapshot)
	candidateOwners := make(map[registry.ID]string, len(owners)+len(changes))
	for id, entryOwner := range owners {
		candidateOwners[canonicalEntryID(id)] = entryOwner
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
			return 0, fmt.Errorf("registry overlay changes contain duplicate entry %s", op.Entry.ID)
		}
		seen[op.Entry.ID] = struct{}{}
		entryOwner, overlayEntry := candidateOwners[op.Entry.ID]
		switch op.Kind {
		case registry.EntryCreate:
			if overlayEntry {
				return 0, fmt.Errorf("overlay entry %s is owned by %s", op.Entry.ID, entryOwner)
			}
			if _, exists := effective[op.Entry.ID]; exists {
				return 0, fmt.Errorf("overlay entry %s shadows durable state", op.Entry.ID)
			}
		case registry.EntryUpdate, registry.EntryDelete:
			if !overlayEntry || entryOwner != owner {
				return 0, fmt.Errorf("overlay owner %s cannot mutate entry %s", owner, op.Entry.ID)
			}
		default:
			return 0, fmt.Errorf("unknown overlay operation %s", op.Kind)
		}
		if op.Kind != registry.EntryDelete {
			if err := validateOverlayEntryProvenance(op.Entry); err != nil {
				return 0, err
			}
			op.Entry.Meta = attrs.NewBagFrom(op.Entry.Meta)
			op.Entry.Meta[overlayOwnerMeta] = owner
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
	if err := r.validateRemovedOverlayDependencies(canonicalStateMap(snapshot), effective, deleted); err != nil {
		return 0, err
	}
	if err := r.validateOverlayComposition(effective, candidateOwners); err != nil {
		return 0, err
	}

	sorted, err := r.sortWithIndex(snapshot, changes)
	if err != nil {
		return 0, NewSortChangesError(err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	newState, err := r.runner.Transition(ctx, r.state, sorted)
	if err != nil {
		if newState != nil && ctx.Err() == nil {
			if rollbackErr := r.rollback(ctx, newState, r.state); rollbackErr != nil {
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
	return nextGeneration, nil
}

// bumpOverlayGeneration issues process-unique generation tokens without
// retaining tombstones for owners whose overlays are empty. An absent owner
// observes the global epoch, so a snapshot opened before a create/delete cycle
// cannot resurrect stale state.
func (r *Reg) bumpOverlayGeneration(owner string) uint64 {
	r.overlayEpoch++
	if len(r.overlays[owner]) == 0 {
		delete(r.overlayGeneration, owner)
		return r.overlayEpoch
	}
	r.overlayGeneration[owner] = r.overlayEpoch
	return r.overlayEpoch
}

func (r *Reg) validateRemovedOverlayDependencies(before, after registry.StateMap, deleted map[registry.ID]struct{}) error {
	if len(deleted) == 0 {
		return nil
	}
	dependencies := topology.ResolveDependencies(before, r.resolver)
	for _, entry := range before {
		if _, survives := after[entry.ID]; !survives {
			continue
		}
		for _, dependencyID := range dependencies[entry.ID] {
			if _, removed := deleted[dependencyID]; removed {
				return fmt.Errorf("overlay deletion removes %s required by live entry %s", dependencyID, entry.ID)
			}
		}
	}
	return nil
}

func canonicalStateMap(state registry.State) registry.StateMap {
	stateMap := make(registry.StateMap, len(state))
	for _, entry := range state {
		entry.ID = canonicalEntryID(entry.ID)
		stateMap[entry.ID] = entry
	}
	return stateMap
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
		return fmt.Errorf("overlay entry %s cannot be a deployment root", entry.ID)
	}
	for _, key := range []string{"module", "module_version", "module_digest"} {
		if _, exists := entry.Meta[key]; exists {
			return fmt.Errorf("overlay entry %s cannot set managed metadata %s", entry.ID, key)
		}
	}
	return nil
}

func (r *Reg) validateOverlayComposition(effective registry.StateMap, owners map[registry.ID]string) error {
	if len(owners) == 0 {
		return nil
	}
	dependencies := topology.ResolveDependencies(effective, r.resolver)
	for _, entry := range effective {
		sourceOwner, sourceOverlay := owners[entry.ID]
		for _, dependencyID := range dependencies[entry.ID] {
			targetOwner, targetOverlay := owners[dependencyID]
			switch {
			case sourceOverlay && targetOverlay && sourceOwner != targetOwner:
				return fmt.Errorf("overlay entry %s owned by %s depends on entry %s owned by %s", entry.ID, sourceOwner, dependencyID, targetOwner)
			case !sourceOverlay && targetOverlay:
				return fmt.Errorf("durable entry %s depends on overlay entry %s owned by %s", entry.ID, dependencyID, targetOwner)
			}
		}
	}
	return nil
}

func (r *Reg) validateDurableTransitionAgainstOverlays(allOps, historyOps registry.ChangeSet) error {
	_ = historyOps
	if len(r.overlayOwners) == 0 {
		return nil
	}
	for _, op := range allOps {
		id := canonicalEntryID(op.Entry.ID)
		if owner, ok := r.overlayOwners[id]; ok {
			return fmt.Errorf("durable transition targets overlay entry %s owned by %s", op.Entry.ID, owner)
		}
	}
	current := canonicalStateMap(r.state)
	deleted := make(map[registry.ID]struct{})
	for _, op := range allOps {
		if op.Kind == registry.EntryDelete {
			deleted[canonicalEntryID(op.Entry.ID)] = struct{}{}
		}
	}
	dependencies := topology.ResolveDependencies(current, r.resolver)
	for owner, entries := range r.overlays {
		for _, entry := range entries {
			for _, dep := range dependencies[entry.ID] {
				if _, ok := deleted[dep]; ok {
					return fmt.Errorf("durable transition deletes %s required by overlay entry %s owned by %s", dep, entry.ID, owner)
				}
			}
		}
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
		knownOwners[canonicalEntryID(id)] = entryOwner
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
		knownOwners[canonicalEntryID(id)] = owner
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
		id := canonicalEntryID(entry.ID)
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
