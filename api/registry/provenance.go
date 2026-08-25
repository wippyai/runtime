// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrMissingProvenance reports a state entry without a provenance record — a
// violation of the ProvMap total-map invariant.
var ErrMissingProvenance = fmt.Errorf("entry has no provenance record")

// NewMissingProvenanceError names the entry that violates the total-map
// invariant.
func NewMissingProvenanceError(id ID) error {
	return fmt.Errorf("%s: %w", id.String(), ErrMissingProvenance)
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

// ProvMap is the provenance of one State: one record per entry. The map is
// total — every entry of the accompanying State has a key. A missing key is an
// invariant violation and must fail loud, never be read as host-authored.
type ProvMap map[ID]EntryProvenance

// Clone returns an independent copy.
func (m ProvMap) Clone() ProvMap {
	if m == nil {
		return nil
	}
	out := make(ProvMap, len(m))
	for id, p := range m {
		out[id] = p
	}
	return out
}

// ProvenancedState is a State together with its provenance. States and their
// provenance travel together through every boundary: baseline load, replay,
// directive inputs, planner comparison, and historical snapshots.
type ProvenancedState struct {
	Prov    ProvMap
	Entries State
}

// Validate reports the first entry without a provenance record, enforcing the
// total-map invariant at a state boundary.
func (s ProvenancedState) Validate() error {
	for _, entry := range s.Entries {
		if _, ok := s.Prov[entry.ID]; !ok {
			return NewMissingProvenanceError(entry.ID)
		}
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
	ResidentModules() map[string]EntryProvenance
	// DependencyRoots returns the IDs of the current deployment roots.
	DependencyRoots() []ID
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

// RollbackOutcome collects what a runner's compensation actually did, so the
// registry can reconstruct the provenance of a partially compensated state
// instead of guessing from the map it published before the transition.
type RollbackOutcome struct {
	mu        sync.Mutex
	surviving []Operation
	errs      []error
}

// Record notes one accepted operation's compensation attempt. A failed
// compensation leaves the operation's effect in the state.
func (o *RollbackOutcome) Record(op Operation, compensated bool, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !compensated {
		o.surviving = append(o.surviving, op)
	}
	if err != nil {
		o.errs = append(o.errs, err)
	}
}

// Surviving returns the accepted operations whose effects remain in the state.
func (o *RollbackOutcome) Surviving() []Operation {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]Operation(nil), o.surviving...)
}

// Err joins the compensation failures.
func (o *RollbackOutcome) Err() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return errors.Join(o.errs...)
}

type rollbackOutcomeContextKey struct{}

// WithRollbackOutcome attaches a compensation collector for the runner to fill.
func WithRollbackOutcome(ctx context.Context, o *RollbackOutcome) context.Context {
	return context.WithValue(ctx, rollbackOutcomeContextKey{}, o)
}

// RollbackOutcomeFromContext returns the collector, if the caller supplied one.
func RollbackOutcomeFromContext(ctx context.Context) *RollbackOutcome {
	if ctx == nil {
		return nil
	}
	o, _ := ctx.Value(rollbackOutcomeContextKey{}).(*RollbackOutcome)
	return o
}
