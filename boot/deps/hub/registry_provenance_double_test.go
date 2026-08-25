// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"

	regapi "github.com/wippyai/runtime/api/registry"
)

// registryProvenance stands in for the registry-owned ProvMap in the tests that
// drive the handler through the real registry. The registry owns provenance in
// the finished system; while its directive boundary still hands over a bare
// State, these tests reproduce its rule — the baseline's records, then the
// record each applied operation carries, host-authored for an entry created
// without one — so the end-to-end scenarios keep running against the real
// handler.
type registryProvenance struct {
	records provIndex
}

func newRegistryProvenance(baseline regapi.ProvenancedState) *registryProvenance {
	return &registryProvenance{records: provByKey(baseline.Prov)}
}

func (r *registryProvenance) seed(state regapi.ProvenancedState) {
	for id, record := range state.Prov {
		r.records[idKey(id)] = record
	}
}

func (r *registryProvenance) record(id regapi.ID) regapi.EntryProvenance {
	return r.records[idKey(id)]
}

// state pairs a registry state with the records tracked so far.
func (r *registryProvenance) state(entries regapi.State) regapi.ProvenancedState {
	prov := make(regapi.ProvMap, len(entries))
	for _, entry := range entries {
		prov[entry.ID] = r.records[idKey(entry.ID)]
	}
	return regapi.ProvenancedState{Entries: entries, Prov: prov}
}

// observe records what a plan would commit. A registry Apply can expand more
// than once over the same unchanged state, so a planned delete is not recorded:
// the entry leaves the state with its transition, and state() reads records
// only for entries the state still holds.
func (r *registryProvenance) observe(result regapi.DirectiveResult) {
	for _, scoped := range result.Additional {
		op := scoped.Operation
		switch op.Kind {
		case regapi.EntryCreate, regapi.EntryUpdate:
			if op.Provenance != nil {
				r.records[idKey(op.Entry.ID)] = *op.Provenance
				continue
			}
			// An update without provenance preserves the record; a create
			// without one is host-authored.
			if _, known := r.records[idKey(op.Entry.ID)]; !known {
				r.records[idKey(op.Entry.ID)] = regapi.EntryProvenance{}
			}
		}
	}
}

func (r *registryProvenance) expand(h *DependencyHandler) func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error) {
	return func(ctx context.Context, op regapi.Operation, snapshot regapi.State) (regapi.DirectiveResult, error) {
		result, err := h.Expand(ctx, op, r.state(snapshot))
		if err != nil {
			return result, err
		}
		r.observe(result)
		return result, nil
	}
}

func (r *registryProvenance) expandChanges(h *DependencyHandler) func(context.Context, regapi.ChangeSet, regapi.State) (regapi.DirectiveResult, error) {
	return func(ctx context.Context, changes regapi.ChangeSet, snapshot regapi.State) (regapi.DirectiveResult, error) {
		result, err := h.ExpandChanges(ctx, changes, r.state(snapshot))
		if err != nil {
			return result, err
		}
		r.observe(result)
		return result, nil
	}
}

func (r *registryProvenance) reconcile(h *DependencyHandler) func(context.Context, regapi.State, regapi.State, *regapi.DependencyResolution) (regapi.DirectiveResult, error) {
	return func(ctx context.Context, current, target regapi.State, resolution *regapi.DependencyResolution) (regapi.DirectiveResult, error) {
		result, err := h.ReconcileResolution(ctx, r.state(current), r.state(target), resolution)
		if err != nil {
			return result, err
		}
		r.observe(result)
		return result, nil
	}
}

// residentVersion is the version of the artifact the tracked records name for
// an entry.
func (r *registryProvenance) residentVersion(id regapi.ID) string {
	return r.record(id).Version
}
