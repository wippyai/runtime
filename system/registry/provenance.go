// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/registry/topology"
)

// applyOpsToProvenance folds a changeset into a provenance map and returns a
// new map; the input is not mutated. Rules: a create takes the operation's
// provenance, or an explicit host record when it carries none; an update with
// provenance replaces the record and one without preserves it; a delete clears
// it. An update of an entry with no existing record backfills the host record,
// keeping the map total for states assembled before provenance existed.
func applyOpsToProvenance(prov registry.ProvMap, ops registry.ChangeSet) registry.ProvMap {
	out := prov.Clone()
	if out == nil {
		out = make(registry.ProvMap, len(ops))
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
				out[id] = registry.EntryProvenance{}
			}
		case registry.EntryDelete:
			delete(out, id)
		}
	}
	return out
}

// annotateChangeSet fills the provenance pair on operations that lack it, from
// the maps of the states the changeset transitions between. Operations the
// dependency directive emitted keep the provenance they carry; synthesized
// deltas (rollback, version transitions) are annotated entirely from the maps.
func annotateChangeSet(ops registry.ChangeSet, from, to registry.ProvMap) {
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
// otherwise. It reconciles the provenance after a failed compensation left a
// partial state.
func provenanceForState(state registry.State, prev registry.ProvMap, ops registry.ChangeSet) registry.ProvMap {
	byOp := make(map[registry.ID]*registry.EntryProvenance, len(ops))
	for i := range ops {
		if ops[i].Provenance != nil {
			byOp[ops[i].Entry.ID] = ops[i].Provenance
		}
	}
	out := make(registry.ProvMap, len(state))
	for _, entry := range state {
		if p, ok := prev[entry.ID]; ok {
			out[entry.ID] = p
			continue
		}
		if p, ok := byOp[entry.ID]; ok {
			out[entry.ID] = *p
			continue
		}
		out[entry.ID] = registry.EntryProvenance{}
	}
	return out
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
func (r *Reg) ResidentModules() map[string]registry.EntryProvenance {
	snap := r.prov.Load()
	if snap == nil {
		return nil
	}
	out := make(map[string]registry.EntryProvenance)
	for _, p := range *snap {
		if p.Module == "" {
			continue
		}
		out[p.Module] = registry.EntryProvenance{Module: p.Module, Version: p.Version, Digest: p.Digest}
	}
	return out
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
	return roots
}

// Provenance returns the current provenance map. The returned map is the live
// immutable snapshot and must not be mutated.
func (r *Reg) Provenance() registry.ProvMap {
	snap := r.prov.Load()
	if snap == nil {
		return nil
	}
	return *snap
}

// publishProvenance swaps the provenance snapshot. Called with r.mu held at
// the same points the state is swapped, so state and provenance move together.
func (r *Reg) publishProvenance(prov registry.ProvMap) {
	r.prov.Store(&prov)
}

// SetDependencyRoot flips the deployment-root selection of one ns.dependency
// entry as a provenance-carrying operation, atomically against concurrent
// applies: the current entry and record are read under the apply serialization
// the operation commits with, so a concurrent module update cannot interleave.
func (r *Reg) SetDependencyRoot(ctx context.Context, id registry.ID, root bool) (registry.Version, error) {
	id = canonicalEntryID(id)

	r.applyMu.Lock()
	entry, getErr := r.GetEntry(id)
	if getErr != nil {
		r.applyMu.Unlock()
		return nil, getErr
	}
	if entry.Kind != registry.NamespaceDependency {
		r.applyMu.Unlock()
		return nil, topology.NewInvalidOperationError(errNotDependencyEntry)
	}
	current, ok := r.EntryProvenance(id)
	if !ok {
		r.applyMu.Unlock()
		return nil, registry.NewMissingProvenanceError(id)
	}
	if current.Root == root {
		version := r.currentVersion
		r.applyMu.Unlock()
		return version, nil
	}
	next := current
	next.Root = root
	op := registry.Operation{
		Kind:               registry.EntryUpdate,
		Entry:              entry,
		Provenance:         &next,
		OriginalProvenance: &current,
	}
	r.applyMu.Unlock()

	// Apply re-reads under its own serialization; the tuple travels on the
	// operation, so a raced module update surfaces as a concurrent-apply
	// error rather than silently rewriting ownership.
	return r.Apply(ctx, registry.ChangeSet{op})
}

var errNotDependencyEntry = fmt.Errorf("only ns.dependency entries select deployment roots")
