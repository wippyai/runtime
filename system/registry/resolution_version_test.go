// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

// resolutionStubDirective answers every expansion with a fixed dependency
// resolution and no additional operations.
type resolutionStubDirective struct {
	resolution *registry.DependencyResolution
}

func (d resolutionStubDirective) Expand(_ context.Context, op registry.Operation, _ registry.ProvenancedState) (registry.DirectiveResult, error) {
	return registry.DirectiveResult{
		Applied:    true,
		Resolution: d.resolution,
		Additional: []registry.ScopedOperation{{
			Operation: op,
			Scope:     registry.ScopeHistory,
		}},
	}, nil
}

func newResolutionTestRegistry(t *testing.T, res *registry.DependencyResolution) *Reg {
	t.Helper()
	hist := historymem.New()
	require.NoError(t, hist.Save(version.New(registry.RootVersion), registry.ChangeSet{}, true))
	runner := NewMockRunner()
	runner.RunFunc = func(state registry.State, cs registry.ChangeSet) (registry.State, error) {
		m := map[registry.ID]registry.Entry{}
		for _, e := range state {
			m[e.ID] = e
		}
		for _, op := range cs {
			switch op.Kind {
			case registry.EntryCreate, registry.EntryUpdate:
				m[op.Entry.ID] = op.Entry
			case registry.EntryDelete:
				delete(m, op.Entry.ID)
			}
		}
		out := registry.State{}
		for _, e := range m {
			out = append(out, e)
		}
		return out, nil
	}
	builder := topology.NewStateBuilder(zap.NewNop(), nil)
	return NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop(),
		WithKindDirective("dep.kind", resolutionStubDirective{resolution: res}))
}

// TestApplyRepeatedResolutionAllocatesNoExtraVersion pins the versioning rule:
// a plan returning the resolution already in effect is not a change, so a
// second identical apply advances only by its own history operations and an
// unchanged canonical digest never forces a version by itself.
func TestApplyRepeatedResolutionAllocatesNoExtraVersion(t *testing.T) {
	res := (&registry.DependencyResolution{
		InputDigest: "sha256:input",
		Roots:       []registry.DependencyRoot{{ID: "app:dep", Component: "acme/mod", Version: "1.0.0"}},
		Modules:     []registry.ResolvedModule{{Name: "acme/mod", Version: "1.0.0", Digest: "sha256:a"}},
	}).Canonical()
	reg := newResolutionTestRegistry(t, res)

	depID := registry.NewID("app", "dep")
	v1, err := reg.Apply(t.Context(), registry.ChangeSet{{
		Kind:  registry.EntryCreate,
		Entry: registry.Entry{ID: depID, Kind: "dep.kind", Data: payloadNew("v1")},
	}})
	require.NoError(t, err)
	require.NotNil(t, v1)

	require.NotNil(t, reg.currentResolution)
	assert.Equal(t, res.Digest, reg.currentResolution.Digest)

	// The same declarative content again: the directive re-returns the same
	// canonical resolution. resolutionChanged must be false — the version
	// advances only because the update itself is a history operation.
	v2, err := reg.Apply(t.Context(), registry.ChangeSet{{
		Kind:  registry.EntryUpdate,
		Entry: registry.Entry{ID: depID, Kind: "dep.kind", Data: payloadNew("v2")},
	}})
	require.NoError(t, err)
	assert.Equal(t, v1.ID()+1, v2.ID())
	assert.Equal(t, res.Digest, reg.currentResolution.Digest)
}

// TestResolutionChangeIsDigestInequality pins that a NEW canonical resolution
// digest marks the transition as resolution-changed and lands in
// currentResolution.
func TestResolutionChangeIsDigestInequality(t *testing.T) {
	first := (&registry.DependencyResolution{
		InputDigest: "sha256:one",
		Roots:       []registry.DependencyRoot{{ID: "app:dep", Component: "acme/mod", Version: "1.0.0"}},
		Modules:     []registry.ResolvedModule{{Name: "acme/mod", Version: "1.0.0", Digest: "sha256:a"}},
	}).Canonical()
	second := (&registry.DependencyResolution{
		InputDigest: "sha256:two",
		Roots:       []registry.DependencyRoot{{ID: "app:dep", Component: "acme/mod", Version: "2.0.0"}},
		Modules:     []registry.ResolvedModule{{Name: "acme/mod", Version: "2.0.0", Digest: "sha256:b"}},
	}).Canonical()
	require.NotEqual(t, first.Digest, second.Digest)

	reg := newResolutionTestRegistry(t, first)
	depID := registry.NewID("app", "dep")
	_, err := reg.Apply(t.Context(), registry.ChangeSet{{
		Kind:  registry.EntryCreate,
		Entry: registry.Entry{ID: depID, Kind: "dep.kind", Data: payloadNew("v1")},
	}})
	require.NoError(t, err)
	require.Equal(t, first.Digest, reg.currentResolution.Digest)

	reg.directivesByKind["dep.kind"] = []registry.Directive{resolutionStubDirective{resolution: second}}
	_, err = reg.Apply(t.Context(), registry.ChangeSet{{
		Kind:  registry.EntryUpdate,
		Entry: registry.Entry{ID: depID, Kind: "dep.kind", Data: payloadNew("v2")},
	}})
	require.NoError(t, err)
	assert.Equal(t, second.Digest, reg.currentResolution.Digest)
}

// resolutionOnlyDirective returns a changed resolution and resident-record
// updates with no module operations — the shape of a module version bump
// whose entries are byte-identical.
type resolutionOnlyDirective struct {
	resolution *registry.DependencyResolution
	resident   registry.ProvMap
}

func (d resolutionOnlyDirective) Expand(_ context.Context, op registry.Operation, _ registry.ProvenancedState) (registry.DirectiveResult, error) {
	return registry.DirectiveResult{
		Applied:    true,
		Resolution: d.resolution,
		Provenance: d.resident,
		Additional: []registry.ScopedOperation{{Operation: op, Scope: registry.ScopeHistory}},
	}, nil
}

// TestIdenticalContentVersionBumpTouchesNoResidentEntry pins the incident this
// change exists for: a module version bump whose entries are byte-identical
// updates the dependency declaration and the resolution, while the module's
// resident entries receive no operation — no manager hears anything about
// them and no instance is recreated.
func TestIdenticalContentVersionBumpTouchesNoResidentEntry(t *testing.T) {
	first := (&registry.DependencyResolution{
		InputDigest: "sha256:one",
		Roots:       []registry.DependencyRoot{{ID: "app:dep", Component: "acme/mod", Version: "1.0.0"}},
		Modules:     []registry.ResolvedModule{{Name: "acme/mod", Version: "1.0.0", Digest: "sha256:a"}},
	}).Canonical()
	second := (&registry.DependencyResolution{
		InputDigest: "sha256:two",
		Roots:       []registry.DependencyRoot{{ID: "app:dep", Component: "acme/mod", Version: "1.0.1"}},
		Modules:     []registry.ResolvedModule{{Name: "acme/mod", Version: "1.0.1", Digest: "sha256:b"}},
	}).Canonical()

	reg := newResolutionTestRegistry(t, first)
	depID := registry.NewID("app", "dep")
	storeID := registry.NewID("acme.mod", "cache")

	// Install: the declaration plus one module-owned resident entry.
	_, err := reg.Apply(t.Context(), registry.ChangeSet{
		{
			Kind:  registry.EntryCreate,
			Entry: registry.Entry{ID: depID, Kind: "dep.kind", Data: payloadNew("v: 1.0.0")},
		},
		{
			Kind:       registry.EntryCreate,
			Entry:      registry.Entry{ID: storeID, Kind: "store.kind", Data: payloadNew("cfg")},
			Provenance: &registry.EntryProvenance{Module: "acme/mod", Version: "1.0.0", Digest: "sha256:a"},
		},
	})
	require.NoError(t, err)
	v1, err := reg.Current()
	require.NoError(t, err)

	storeProvBefore, ok := reg.EntryProvenance(storeID)
	require.True(t, ok)

	// The bump: the declaration changes, the module's entries are identical,
	// so the directive emits the declaration update, the new resolution, and
	// the resident-record advance — no operation for the store entry.
	bumped := registry.EntryProvenance{Module: "acme/mod", Version: "1.0.1", Digest: "sha256:b"}
	reg.directivesByKind["dep.kind"] = []registry.Directive{resolutionOnlyDirective{
		resolution: second,
		resident:   registry.ProvMap{storeID: bumped},
	}}
	runner := reg.runner.(*MockRunner)
	var dispatched registry.ChangeSet
	prevRun := runner.RunFunc
	runner.RunFunc = func(state registry.State, cs registry.ChangeSet) (registry.State, error) {
		dispatched = append(dispatched, cs...)
		return prevRun(state, cs)
	}

	v2, err := reg.Apply(t.Context(), registry.ChangeSet{{
		Kind:  registry.EntryUpdate,
		Entry: registry.Entry{ID: depID, Kind: "dep.kind", Data: payloadNew("v: 1.0.1")},
	}})
	require.NoError(t, err)

	assert.Equal(t, v1.ID()+1, v2.ID())
	assert.Equal(t, second.Digest, reg.currentResolution.Digest)
	for _, op := range dispatched {
		assert.NotEqual(t, storeID, op.Entry.ID.Canonical(),
			"the module's resident entry must receive no operation for identical content")
	}
	storeProvAfter, ok := reg.EntryProvenance(storeID)
	require.True(t, ok)
	assert.NotEqual(t, storeProvBefore, storeProvAfter, "the resident record advances with the bump")
	assert.Equal(t, bumped, storeProvAfter)
}
