// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/registry/topology"
)

// overlayShadow records one overlay's claim over a durable entry: the durable
// content the claim displaces and whether the claim also removes that entry
// from effective state. Releasing the claim restores original.
type overlayShadow struct {
	owner    string
	original registry.Entry
	removed  bool
}

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
	base := r.baseLocked()
	snapshot := append(registry.State(nil), r.state...)
	currentGeneration, activeOwner := r.overlayGeneration[owner]
	if !activeOwner {
		currentGeneration = r.overlayFloor
	}
	owners := make(map[registry.ID]string, len(r.overlayOwners))
	for id, value := range r.overlayOwners {
		owners[id] = value
	}
	shadows := make(map[registry.ID]overlayShadow, len(r.overlayShadows))
	for id, value := range r.overlayShadows {
		shadows[id] = value
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
	candidateShadows := make(map[registry.ID]overlayShadow, len(shadows)+len(changes))
	for id, shadow := range shadows {
		candidateShadows[id] = shadow
	}
	transition := make(registry.ChangeSet, 0, len(changes))
	seen := make(map[registry.ID]struct{}, len(changes))
	deleted := make(map[registry.ID]struct{})
	for i := range changes {
		op := &changes[i]
		op.Entry = cloneOverlayEntry(op.Entry)
		if op.OriginalEntry != nil {
			original := cloneOverlayEntry(*op.OriginalEntry)
			op.OriginalEntry = &original
		}
		id := op.Entry.ID
		if _, duplicate := seen[id]; duplicate {
			return 0, NewOverlayValidationError("registry overlay changes contain a duplicate entry", map[string]any{"entry_id": id.String()})
		}
		seen[id] = struct{}{}
		entryOwner, overlayEntry := candidateOwners[id]
		shadow, shadowed := candidateShadows[id]
		durable, resident := effective[id]
		switch op.Kind {
		case registry.EntryCreate:
			if overlayEntry {
				return 0, NewOverlayConflictError("registry overlay entry is already owned", map[string]any{"entry_id": id.String(), "owner": entryOwner})
			}
			if resident {
				return 0, NewOverlayConflictError("registry overlay entry conflicts with durable state", map[string]any{"entry_id": id.String()})
			}
		case registry.EntryUpdate, registry.EntryDelete:
			if overlayEntry && entryOwner != owner {
				return 0, NewOverlayConflictError("registry overlay owner cannot mutate entry", map[string]any{"entry_id": id.String(), "owner": owner})
			}
			if !overlayEntry && !resident {
				return 0, NewOverlayConflictError("registry overlay owner cannot mutate entry", map[string]any{"entry_id": id.String(), "owner": owner})
			}
		default:
			return 0, NewOverlayValidationError("unknown registry overlay operation", map[string]any{"operation": op.Kind})
		}

		// A shadow claims a durable entry: the claimed content is what the
		// registry restores when the claim is released.
		var claimed *registry.Entry
		switch {
		case shadowed:
			original := shadow.original
			claimed = &original
		case !overlayEntry:
			original := cloneOverlayEntry(durable)
			claimed = &original
		}
		if claimed != nil {
			if err := r.validateOverlayKind(id, claimed.Kind); err != nil {
				return 0, err
			}
		}
		if op.Kind != registry.EntryDelete {
			if err := validateOverlayEntryMetadata(op.Entry, claimed); err != nil {
				return 0, err
			}
			if err := r.validateOverlayKind(id, op.Entry.Kind); err != nil {
				return 0, err
			}
		}

		switch op.Kind {
		case registry.EntryCreate:
			effective[id] = op.Entry
			candidateOwners[id] = owner
			transition = append(transition, registry.Operation{Kind: registry.EntryCreate, Entry: op.Entry})
		case registry.EntryUpdate:
			entry := op.Entry
			kind := registry.EntryUpdate
			if claimed != nil {
				entry.Registry = claimed.Registry
				candidateShadows[id] = overlayShadow{owner: owner, original: *claimed}
				if shadowed && shadow.removed {
					kind = registry.EntryCreate
				}
			}
			effective[id] = entry
			candidateOwners[id] = owner
			transition = append(transition, registry.Operation{Kind: kind, Entry: entry})
		case registry.EntryDelete:
			switch {
			case shadowed:
				kind := registry.EntryUpdate
				if shadow.removed {
					kind = registry.EntryCreate
				}
				effective[id] = shadow.original
				delete(candidateOwners, id)
				delete(candidateShadows, id)
				transition = append(transition, registry.Operation{Kind: kind, Entry: shadow.original})
			case overlayEntry:
				delete(effective, id)
				delete(candidateOwners, id)
				deleted[id] = struct{}{}
				transition = append(transition, registry.Operation{Kind: registry.EntryDelete, Entry: op.Entry})
			default:
				candidateOwners[id] = owner
				candidateShadows[id] = overlayShadow{owner: owner, original: *claimed, removed: true}
				delete(effective, id)
				deleted[id] = struct{}{}
				transition = append(transition, registry.Operation{Kind: registry.EntryDelete, Entry: *claimed})
			}
		}
	}
	if len(deleted) != 0 {
		if err := r.validateRemovedOverlayDependencies(topology.NewStateMap(snapshot), effective, deleted); err != nil {
			return 0, err
		}
	}
	if err := r.validateOverlayComposition(effective, candidateOwners, candidateShadows); err != nil {
		return 0, err
	}

	sorted, err := r.sortWithIndex(snapshot, transition)
	if err != nil {
		return 0, NewSortChangesError(err)
	}

	// Handlers run with r.mu free. applyMu is held for the whole call, so the
	// captured base stays the registry's live state until this overlay publishes.
	newState, err := r.runner.Transition(ctx, base.state, sorted)
	if err != nil {
		if newState != nil && ctx.Err() == nil {
			if rollbackErr := r.rollback(ctx, newState, base.state); rollbackErr != nil {
				r.mu.Lock()
				r.reconcileOverlayIndexesAfterFailedRollback(owner, owners, candidateOwners, shadows, candidateShadows)
				r.mu.Unlock()
				return 0, NewApplyChangesError(err, rollbackErr)
			}
		}
		return 0, NewApplyChangesError(err, nil)
	}

	var nextGeneration uint64
	if publishErr := r.publish(base, func() {
		r.rebuildOverlayIndexes(candidateOwners, candidateShadows, newState)
		nextGeneration = r.bumpOverlayGeneration(owner)
		r.state = newState
		r.rebuildIndex()
		r.patchDepIndex(sorted)
	}); publishErr != nil {
		return 0, publishErr
	}
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

// validateOverlayEntryMetadata keeps registry metadata out of overlay-authored
// entries. A shadow carries the claimed durable entry's metadata, so an owner
// may round-trip the entry it reads back from its overlay.
func validateOverlayEntryMetadata(entry registry.Entry, claimed *registry.Entry) error {
	if entry.Registry == (registry.EntryMetadata{}) {
		return nil
	}
	if claimed != nil && entry.Registry == claimed.Registry {
		return nil
	}
	return NewOverlayValidationError("registry overlay entry cannot set registry metadata", map[string]any{
		"entry_id": entry.ID.String(),
	})
}

// validateOverlayKind keeps directive-owned kinds out of overlays, both for
// overlay-authored entries and for the durable entries a shadow claims.
func (r *Reg) validateOverlayKind(id registry.ID, kind registry.Kind) error {
	if len(r.directivesByKind[kind]) == 0 {
		return nil
	}
	return NewOverlayValidationError("registry overlay entries cannot use directive-owned kinds", map[string]any{
		"entry_id": id.String(),
		"kind":     kind,
	})
}

// validateOverlayComposition keeps process-local entries out of the durable
// dependency graph. A shadowed entry is exempt as a target: the durable entry
// is still resident, only its content is process-local.
func (r *Reg) validateOverlayComposition(effective registry.StateMap, owners map[registry.ID]string, shadows map[registry.ID]overlayShadow) error {
	if len(owners) == 0 {
		return nil
	}
	return topology.VisitDependencies(effective, r.resolver, func(source, target registry.ID) error {
		sourceOwner, sourceOverlay := owners[source]
		targetOwner, targetOverlay := owners[target]
		_, targetShadow := shadows[target]
		switch {
		case sourceOverlay && targetOverlay && sourceOwner != targetOwner:
			return NewOverlayConflictError("registry overlay dependency crosses owner boundary", map[string]any{
				"entry_id":      source.String(),
				"owner":         sourceOwner,
				"dependency_id": target.String(),
				"target_owner":  targetOwner,
			})
		case !sourceOverlay && targetOverlay && !targetShadow:
			return NewOverlayConflictError("durable registry entry depends on process-local overlay", map[string]any{
				"entry_id":      source.String(),
				"dependency_id": target.String(),
				"owner":         targetOwner,
			})
		}
		return nil
	})
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
	return r.validateOverlayComposition(current, r.overlayOwners, r.overlayShadows)
}

// composeOverlays folds process-local state onto one durable version and
// returns the shadow claims rebased on that version. The caller commits the
// returned claims only once the transition succeeds, so a refused version
// selection leaves the live claims untouched.
func (r *Reg) composeOverlays(stateMap registry.StateMap) (map[registry.ID]overlayShadow, error) {
	for id, entry := range stateMap {
		canonicalID := canonicalEntryID(id)
		entry.ID = canonicalEntryID(entry.ID)
		if !canonicalID.Equal(entry.ID) {
			return nil, NewOverlayValidationError("durable state key does not match its entry", map[string]any{"key": id.String(), "entry_id": entry.ID.String()})
		}
		if canonicalID != id {
			delete(stateMap, id)
			if _, duplicate := stateMap[canonicalID]; duplicate {
				return nil, NewOverlayValidationError("selected durable version contains a duplicate entry", map[string]any{"entry_id": canonicalID.String()})
			}
			stateMap[canonicalID] = entry
		}
	}
	shadows := make(map[registry.ID]overlayShadow, len(r.overlayShadows))
	for id, shadow := range r.overlayShadows {
		durable, resident := stateMap[id]
		if !resident {
			return nil, NewOverlayConflictError("selected durable version removes a shadowed registry entry", map[string]any{
				"entry_id": id.String(),
				"owner":    shadow.owner,
			})
		}
		shadows[id] = overlayShadow{owner: shadow.owner, original: durable, removed: shadow.removed}
		if shadow.removed {
			delete(stateMap, id)
		}
	}
	for owner, entries := range r.overlays {
		for _, entry := range entries {
			entry.ID = canonicalEntryID(entry.ID)
			if shadow, isShadow := shadows[entry.ID]; isShadow {
				entry.Registry = shadow.original.Registry
				stateMap[entry.ID] = entry
				continue
			}
			if _, exists := stateMap[entry.ID]; exists {
				return nil, NewOverlayConflictError("overlay entry conflicts with the selected durable version", map[string]any{"entry_id": entry.ID.String(), "owner": owner})
			}
			stateMap[entry.ID] = entry
		}
	}
	return shadows, r.validateOverlayComposition(stateMap, r.overlayOwners, shadows)
}

// reconcileOverlayIndexesAfterFailedRollback rebuilds the overlay indexes from
// the claims that were live before the changeset plus the ones it would have
// created, and lets the rebuild decide which of them the desynced state kept.
func (r *Reg) reconcileOverlayIndexesAfterFailedRollback(
	owner string,
	previousOwners, candidateOwners map[registry.ID]string,
	previousShadows, candidateShadows map[registry.ID]overlayShadow,
) {
	knownOwners := make(map[registry.ID]string, len(previousOwners)+len(candidateOwners))
	ownerGenerationInvalidated := false
	for id, entryOwner := range previousOwners {
		knownOwners[id] = entryOwner
		if entryOwner == owner {
			ownerGenerationInvalidated = true
		}
	}
	for id, entryOwner := range candidateOwners {
		knownOwners[id] = entryOwner
	}
	knownShadows := make(map[registry.ID]overlayShadow, len(previousShadows)+len(candidateShadows))
	for id, shadow := range previousShadows {
		knownShadows[id] = shadow
	}
	for id, shadow := range candidateShadows {
		knownShadows[id] = shadow
	}

	r.rebuildOverlayIndexes(knownOwners, knownShadows, r.state)
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
	knownShadows := make(map[registry.ID]overlayShadow, len(r.overlayShadows))
	for id, shadow := range r.overlayShadows {
		knownShadows[id] = shadow
	}
	r.rebuildOverlayIndexes(knownOwners, knownShadows, r.state)
	for owner := range owners {
		r.bumpOverlayGeneration(owner)
	}
}

// rebuildOverlayIndexes keeps the claims the resident state actually carries.
// A shadow that removes its durable entry has no resident entry to find, so it
// is kept exactly while that entry is absent.
func (r *Reg) rebuildOverlayIndexes(knownOwners map[registry.ID]string, knownShadows map[registry.ID]overlayShadow, effective registry.State) {
	nextOwners := make(map[registry.ID]string)
	nextShadows := make(map[registry.ID]overlayShadow)
	nextOverlays := make(map[string]registry.StateMap)
	resident := make(map[registry.ID]struct{}, len(effective))
	for _, entry := range effective {
		id := entry.ID
		resident[id] = struct{}{}
		entryOwner, isOverlay := knownOwners[id]
		if !isOverlay {
			continue
		}
		if shadow, isShadow := knownShadows[id]; isShadow {
			if shadow.removed {
				continue
			}
			nextShadows[id] = shadow
		}
		entry.ID = id
		nextOwners[id] = entryOwner
		if nextOverlays[entryOwner] == nil {
			nextOverlays[entryOwner] = make(registry.StateMap)
		}
		nextOverlays[entryOwner][id] = cloneOverlayEntry(entry)
	}
	for id, shadow := range knownShadows {
		if !shadow.removed {
			continue
		}
		if _, present := resident[id]; present {
			continue
		}
		if _, isOverlay := knownOwners[id]; !isOverlay {
			continue
		}
		nextOwners[id] = shadow.owner
		nextShadows[id] = shadow
	}
	r.overlayOwners = nextOwners
	r.overlayShadows = nextShadows
	r.overlays = make(map[string]registry.State, len(nextOverlays))
	for entryOwner, entries := range nextOverlays {
		r.overlays[entryOwner] = topology.StateMapToSlice(entries)
	}
}

var _ registry.OverlayWriter = (*Reg)(nil)
