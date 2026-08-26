// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// ErrMissingProvenance reports a state entry without a provenance record — a
// violation of the ProvenanceMap total-map invariant.
var ErrMissingProvenance = errors.New("entry has no provenance record")

// ErrOrphanedProvenance reports a provenance record without a corresponding
// state entry — also a violation of the one-record-per-entry invariant.
var ErrOrphanedProvenance = errors.New("provenance record has no entry")

// ErrConflictingModuleProvenance reports entries that attribute one module to
// different resident artifact identities.
var ErrConflictingModuleProvenance = errors.New("module has conflicting provenance records")

// NewMissingProvenanceError names the entry that violates the total-map
// invariant.
func NewMissingProvenanceError(id ID) error {
	return fmt.Errorf("%s: %w", id.String(), ErrMissingProvenance)
}

// NewOrphanedProvenanceError names the record that has no state entry.
func NewOrphanedProvenanceError(id ID) error {
	return fmt.Errorf("%s: %w", id.String(), ErrOrphanedProvenance)
}

// NewConflictingModuleProvenanceError names the module whose entries disagree.
func NewConflictingModuleProvenanceError(module string) error {
	return fmt.Errorf("%s: %w", module, ErrConflictingModuleProvenance)
}

// EntryProvenance is what the runtime knows ABOUT an entry, held by the
// registry outside the author-owned entry payload: which installed module
// produced it, at which artifact identity, and whether its ns.dependency
// declaration is a deployment root. For a replacement (local dev source) the
// digest is the source tree identity and Version may be empty; Module plus
// Digest then identify the artifact.
type EntryProvenance struct {
	// Module names the owning module as "org/name". Empty means the entry is
	// host-authored: authored by the application or a user, owned by no module.
	Module string `json:"module,omitempty"`
	// Version is the owning module's version at the time this entry was
	// materialized.
	Version string `json:"version,omitempty"`
	// Digest is the identity of the artifact that produced this entry's bytes.
	Digest string `json:"digest,omitempty"`
	// Root marks an ns.dependency entry selected as a deployment root. Roots
	// are deployment-context state: the same declaration is a root when its
	// module is the deployed application and a transitive link otherwise.
	Root bool `json:"root,omitempty"`
}

// HostAuthored reports whether the entry is owned by no module.
func (p EntryProvenance) HostAuthored() bool {
	return p.Module == ""
}

// ProvenanceMap is the provenance of one State: one record per entry. The map is
// total — every entry of the accompanying State has a key. A missing key is an
// invariant violation and must fail loudly, never be read as host-authored.
type ProvenanceMap map[ID]EntryProvenance

// Clone returns an independent copy.
func (m ProvenanceMap) Clone() ProvenanceMap {
	if m == nil {
		return nil
	}
	out := make(ProvenanceMap, len(m))
	for id, p := range m {
		out[id] = p
	}
	return out
}

// ProvenancedState is a State together with its provenance. States and their
// provenance travel together through every boundary: baseline load, replay,
// directive inputs, planner comparison, and historical snapshots.
type ProvenancedState struct {
	Provenance ProvenanceMap
	Entries    State
}

// Validate reports the first entry without a provenance record, enforcing the
// total-map invariant at a state boundary.
func (s ProvenancedState) Validate() error {
	entries := make(map[ID]struct{}, len(s.Entries))
	modules := make(map[string]EntryProvenance)
	for _, entry := range s.Entries {
		p, ok := s.Provenance[entry.ID]
		if !ok {
			return NewMissingProvenanceError(entry.ID)
		}
		entries[entry.ID] = struct{}{}
		if p.Module == "" {
			continue
		}
		if resident, exists := modules[p.Module]; exists &&
			(resident.Version != p.Version || resident.Digest != p.Digest) {
			return NewConflictingModuleProvenanceError(p.Module)
		}
		modules[p.Module] = p
	}
	orphaned := make([]ID, 0)
	for id := range s.Provenance {
		if _, ok := entries[id]; !ok {
			orphaned = append(orphaned, id)
		}
	}
	if len(orphaned) != 0 {
		sort.Slice(orphaned, func(i, j int) bool {
			return orphaned[i].String() < orphaned[j].String()
		})
		return NewOrphanedProvenanceError(orphaned[0])
	}
	return nil
}

// ProvenanceReader answers provenance questions about the live registry state.
// Implementations serve reads from an immutable snapshot, so a reader inside a
// listener callback never blocks a transition in flight.
type ProvenanceReader interface {
	// EntryProvenance returns the record for one entry of the current state.
	EntryProvenance(ID) (EntryProvenance, bool)
	// ResidentModules folds the current provenance into module -> identity of
	// the artifact whose entries are resident.
	ResidentModules() (map[string]EntryProvenance, error)
	// DependencyRoots returns the IDs of the current deployment roots.
	DependencyRoots() []ID
}

// ProvenancedSnapshotReader serves complete current and historical registry
// states with their ownership records. Each result must describe one registry
// version atomically; consumers must not reconstruct it from separate entry
// and provenance reads.
type ProvenancedSnapshotReader interface {
	SnapshotState() (Version, ProvenancedState, error)
	ProvenancedStateAtVersion(context.Context, Version) (ProvenancedState, error)
}

// opProvenanceContextKey carries one operation's effective provenance to the
// listeners the transition dispatches, alongside the entry event.
type opProvenanceContextKey struct{}

// OpProvenance is the provenance pair of one in-flight operation: Effective for
// the entry the operation carries, Original for the entry it replaces.
type OpProvenance struct {
	Effective *EntryProvenance
	Original  *EntryProvenance
}

// WithOpProvenance returns an operation-scoped context carrying the provenance
// pair of the operation being dispatched.
func WithOpProvenance(ctx context.Context, p OpProvenance) context.Context {
	return context.WithValue(ctx, opProvenanceContextKey{}, p)
}

// OpProvenanceFromContext returns the provenance pair of the operation being
// dispatched, if the dispatcher supplied one.
func OpProvenanceFromContext(ctx context.Context) (OpProvenance, bool) {
	if ctx == nil {
		return OpProvenance{}, false
	}
	p, ok := ctx.Value(opProvenanceContextKey{}).(OpProvenance)
	return p, ok
}
