// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/registry/topology"
)

// applyOpsToProvenance folds a changeset into a provenance map and returns a
// new map; the input is not mutated. Rules: a create takes the operation's
// provenance, or an explicit host record when it carries none; an update with
// provenance replaces the record and one without preserves it; a delete clears
// it. An update without an existing record is an invariant violation: legacy
// state is normalized at the load boundary, never during a live transition.
func applyOpsToProvenance(prov registry.ProvenanceMap, ops registry.ChangeSet) (registry.ProvenanceMap, error) {
	out := prov.Clone()
	if out == nil {
		out = make(registry.ProvenanceMap, len(ops))
	}
	for _, op := range ops {
		id := op.Entry.ID
		switch op.Kind {
		case registry.EntryCreate:
			if op.Provenance != nil {
				out[id] = *op.Provenance
			} else {
				out[id] = registry.EntryProvenance{}
			}
		case registry.EntryUpdate:
			if op.Provenance != nil {
				out[id] = *op.Provenance
			} else if _, ok := out[id]; !ok {
				return nil, registry.NewMissingProvenanceError(id)
			}
		case registry.EntryDelete:
			delete(out, id)
		}
	}
	return out, nil
}

// applyHistoryOpsToProvenance folds durable operations and upgrades the root
// statement used before registry-owned provenance. New history rows carry
// provenance and never encode DependencyRoot, so the compatibility path is
// limited to provenance-less dependency operations read from history.
func applyHistoryOpsToProvenance(prov registry.ProvenanceMap, ops registry.ChangeSet) (registry.ProvenanceMap, error) {
	out, err := applyOpsToProvenance(prov, ops)
	if err != nil {
		return nil, err
	}
	for _, op := range ops {
		if op.Provenance != nil || op.Entry.Kind != registry.NamespaceDependency || !op.Entry.DependencyRoot {
			continue
		}
		id := canonicalEntryID(op.Entry.ID)
		record, ok := out[id]
		if !ok {
			continue
		}
		record.Root = true
		out[id] = record
	}
	return out, nil
}

// annotateChangeSet fills the provenance pair on operations that lack it, from
// the maps of the states the changeset transitions between. Operations the
// dependency directive emitted keep the provenance they carry; synthesized
// deltas (rollback, version transitions) are annotated entirely from the maps.
func annotateChangeSet(ops registry.ChangeSet, from, to registry.ProvenanceMap) {
	for i := range ops {
		op := &ops[i]
		if op.OriginalProvenance == nil {
			if p, ok := from[op.Entry.ID]; ok {
				cp := p
				op.OriginalProvenance = &cp
			}
		}
		if op.Provenance != nil {
			continue
		}
		switch op.Kind {
		case registry.EntryCreate, registry.EntryUpdate:
			if p, ok := to[op.Entry.ID]; ok {
				cp := p
				op.Provenance = &cp
			}
		case registry.EntryDelete:
			// The effective provenance of a delete is the record being removed.
			if p, ok := from[op.Entry.ID]; ok {
				cp := p
				op.Provenance = &cp
			}
		}
	}
}

// provenanceForState builds a total map for an arbitrary state from the best
// available sources: the previous map for entries that survived, operation
// provenance for entries a changeset introduced, and the host record
// otherwise. Missing ownership is never interpreted as host ownership.
func provenanceForState(state registry.State, prev registry.ProvenanceMap, ops registry.ChangeSet) (registry.ProvenanceMap, error) {
	prev = canonicalProvClone(prev)
	byOp := make(map[registry.ID]registry.EntryProvenance, len(ops))
	for i := range ops {
		id := canonicalEntryID(ops[i].Entry.ID)
		if ops[i].Provenance != nil {
			byOp[id] = *ops[i].Provenance
		} else if ops[i].Kind == registry.EntryCreate {
			byOp[id] = registry.EntryProvenance{}
		}
	}
	out := make(registry.ProvenanceMap, len(state))
	for _, entry := range state {
		id := canonicalEntryID(entry.ID)
		if p, ok := prev[id]; ok {
			out[id] = p
			continue
		}
		if p, ok := byOp[id]; ok {
			out[id] = p
			continue
		}
		return nil, registry.NewMissingProvenanceError(id)
	}
	return out, nil
}

// --- ProvenanceReader ---

// EntryProvenance returns the provenance record for one entry of the current
// state. Reads are served from the atomically swapped snapshot and never take
// the registry lock.
func (r *Reg) EntryProvenance(id registry.ID) (registry.EntryProvenance, bool) {
	snap := r.prov.Load()
	if snap == nil {
		return registry.EntryProvenance{}, false
	}
	p, ok := (*snap)[canonicalEntryID(id)]
	return p, ok
}

// ResidentModules folds the current provenance into module name to resident
// artifact identity.
func (r *Reg) ResidentModules() (map[string]registry.EntryProvenance, error) {
	snap := r.prov.Load()
	if snap == nil {
		return nil, nil
	}
	out := make(map[string]registry.EntryProvenance)
	conflict := ""
	for _, p := range *snap {
		if p.Module == "" {
			continue
		}
		if resident, ok := out[p.Module]; ok &&
			(resident.Version != p.Version || resident.Digest != p.Digest) {
			if conflict == "" || p.Module < conflict {
				conflict = p.Module
			}
		}
		out[p.Module] = registry.EntryProvenance{Module: p.Module, Version: p.Version, Digest: p.Digest}
	}
	if conflict != "" {
		return nil, registry.NewConflictingModuleProvenanceError(conflict)
	}
	return out, nil
}

// DependencyRoots returns the IDs of the current deployment roots.
func (r *Reg) DependencyRoots() []registry.ID {
	snap := r.prov.Load()
	if snap == nil {
		return nil
	}
	var roots []registry.ID
	for id, p := range *snap {
		if p.Root {
			roots = append(roots, id)
		}
	}
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].String() < roots[j].String()
	})
	return roots
}

// provenanceSnapshot returns the live immutable map for registry-internal reads.
// Values crossing a component boundary must clone it.
func (r *Reg) provenanceSnapshot() registry.ProvenanceMap {
	snap := r.prov.Load()
	if snap == nil {
		return nil
	}
	return *snap
}

// publishProvenance swaps the provenance snapshot. Called with r.mu held at
// the same points the state is swapped, so state and provenance move together.
func (r *Reg) publishProvenance(prov registry.ProvenanceMap) {
	owned := canonicalProvClone(prov)
	r.prov.Store(&owned)
}

// SnapshotState returns the current version, entries, and provenance as one
// consistent view: all three are read under the same lock, so a concurrent
// apply cannot produce a mismatched snapshot.
func (r *Reg) SnapshotState() (registry.Version, registry.ProvenancedState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries := make(registry.State, len(r.state))
	copy(entries, r.state)
	return r.currentVersion, registry.ProvenancedState{Entries: entries, Provenance: r.provenanceSnapshot().Clone()}, nil
}

// SetDependencyRoot flips the deployment-root selection of one ns.dependency
// entry as a provenance-carrying operation, atomically against concurrent
// applies: the current entry and record are read under the apply serialization
// the operation commits with, so a concurrent module update cannot interleave.
func (r *Reg) SetDependencyRoot(ctx context.Context, id registry.ID, root bool) (registry.Version, error) {
	id = canonicalEntryID(id)

	// The read and the apply are conditioned on the same version: an apply
	// that lands in between fails the condition and the mutation re-reads,
	// so a stale entry or tuple can never overwrite a concurrent update.
	for attempt := 0; attempt < setRootRetries; attempt++ {
		r.mu.RLock()
		base := r.currentVersion
		r.mu.RUnlock()

		entry, getErr := r.GetEntry(id)
		if getErr != nil {
			return nil, getErr
		}
		if entry.Kind != registry.NamespaceDependency {
			return nil, topology.NewInvalidOperationError(errNotDependencyEntry)
		}
		current, ok := r.EntryProvenance(id)
		if !ok {
			return nil, registry.NewMissingProvenanceError(id)
		}
		if current.Root == root {
			return base, nil
		}
		next := current
		next.Root = root
		op := registry.Operation{
			Kind:               registry.EntryUpdate,
			Entry:              entry,
			Provenance:         &next,
			OriginalProvenance: &current,
		}

		version, applyErr := r.applyFrom(ctx, registry.ChangeSet{op}, base)
		if applyErr == nil {
			return version, nil
		}
		if !errors.Is(applyErr, ErrConcurrentApply) {
			return nil, applyErr
		}
	}
	return nil, NewConcurrentApplyError(0, 0)
}

const setRootRetries = 4

var errNotDependencyEntry = fmt.Errorf("only ns.dependency entries select deployment roots")

// ProvenancedStateAtVersion reconstructs the declarative state and its
// provenance at one stored version: the deployment baseline plus the authored
// history, with provenance folded the same way live replay folds it. Module
// worlds reconciled from stored resolutions materialize only through
// ApplyVersion, which alone may stage their effects; a snapshot answers for
// the declarative layer. The baseline is copied under the lock and history
// replays without it, so a long history never blocks writers.
func (r *Reg) ProvenancedStateAtVersion(ctx context.Context, v registry.Version) (registry.ProvenancedState, error) {
	r.mu.RLock()
	baseline := make(registry.State, len(r.baseline))
	copy(baseline, r.baseline)
	baselineProv := r.baselineProv.Clone()
	r.mu.RUnlock()

	entries, prov, err := replayVersionState(ctx, r.history, baseline, baselineProv, v)
	if err != nil {
		return registry.ProvenancedState{}, err
	}
	return registry.ProvenancedState{Entries: entries, Provenance: prov}, nil
}
