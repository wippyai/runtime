// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

func newTestRegistry(t *testing.T) (*Reg, *MockRunner) {
	t.Helper()
	hist := historymem.New()
	_ = hist.Save(version.New(registry.RootVersion), registry.ChangeSet{}, true)
	runner := NewMockRunner()
	builder := topology.NewStateBuilder(zap.NewNop(), nil)
	return NewRegistry(hist, runner, builder, topology.NewResolver(), zap.NewNop()), runner
}

func payloadNew(v any) payload.Payload {
	return payload.New(v)
}

func provOp(kind string, id registry.ID, p *registry.EntryProvenance) registry.Operation {
	return registry.Operation{
		Kind:       kind,
		Entry:      registry.Entry{ID: id, Kind: "test"},
		Provenance: p,
	}
}

func TestApplyOpsToProvenance(t *testing.T) {
	a := registry.NewID("ns", "a")
	b := registry.NewID("ns", "b")
	owned := &registry.EntryProvenance{Module: "org/mod", Version: "1.0.0", Digest: "sha256:x"}

	t.Run("create with provenance records it", func(t *testing.T) {
		out, err := applyOpsToProvenance(nil, registry.ChangeSet{provOp(registry.EntryCreate, a, owned)})
		require.NoError(t, err)
		assert.Equal(t, *owned, out[a])
	})

	t.Run("create without provenance records the host entry", func(t *testing.T) {
		out, err := applyOpsToProvenance(nil, registry.ChangeSet{provOp(registry.EntryCreate, a, nil)})
		require.NoError(t, err)
		p, ok := out[a]
		require.True(t, ok, "total map: every created entry has a record")
		assert.True(t, p.HostAuthored())
	})

	t.Run("update without provenance preserves the record", func(t *testing.T) {
		prov := registry.ProvMap{a: *owned}
		out, err := applyOpsToProvenance(prov, registry.ChangeSet{provOp(registry.EntryUpdate, a, nil)})
		require.NoError(t, err)
		assert.Equal(t, *owned, out[a])
	})

	t.Run("update with provenance replaces the record", func(t *testing.T) {
		prov := registry.ProvMap{a: *owned}
		next := &registry.EntryProvenance{Module: "org/mod", Version: "2.0.0", Digest: "sha256:y"}
		out, err := applyOpsToProvenance(prov, registry.ChangeSet{provOp(registry.EntryUpdate, a, next)})
		require.NoError(t, err)
		assert.Equal(t, *next, out[a])
	})

	t.Run("update of an unknown entry fails closed", func(t *testing.T) {
		_, err := applyOpsToProvenance(nil, registry.ChangeSet{provOp(registry.EntryUpdate, a, nil)})
		require.ErrorIs(t, err, registry.ErrMissingProvenance)
	})

	t.Run("delete clears the record", func(t *testing.T) {
		prov := registry.ProvMap{a: *owned, b: {}}
		out, err := applyOpsToProvenance(prov, registry.ChangeSet{provOp(registry.EntryDelete, a, nil)})
		require.NoError(t, err)
		_, ok := out[a]
		assert.False(t, ok)
		_, ok = out[b]
		assert.True(t, ok)
	})

	t.Run("input map is not mutated", func(t *testing.T) {
		prov := registry.ProvMap{a: *owned}
		_, err := applyOpsToProvenance(prov, registry.ChangeSet{provOp(registry.EntryDelete, a, nil)})
		require.NoError(t, err)
		assert.Contains(t, prov, a)
	})

	t.Run("fold is deterministic over composition", func(t *testing.T) {
		ops1 := registry.ChangeSet{provOp(registry.EntryCreate, a, owned)}
		ops2 := registry.ChangeSet{provOp(registry.EntryDelete, a, nil), provOp(registry.EntryCreate, b, nil)}
		intermediate, err := applyOpsToProvenance(nil, ops1)
		require.NoError(t, err)
		once, err := applyOpsToProvenance(intermediate, ops2)
		require.NoError(t, err)
		all, err := applyOpsToProvenance(nil, append(append(registry.ChangeSet{}, ops1...), ops2...))
		require.NoError(t, err)
		assert.Equal(t, all, once)
	})
}

func TestAnnotateChangeSet(t *testing.T) {
	a := registry.NewID("ns", "a")
	from := registry.ProvMap{a: {Module: "org/mod", Version: "1.0.0"}}
	to := registry.ProvMap{a: {Module: "org/mod", Version: "2.0.0"}}

	t.Run("update is annotated from both maps", func(t *testing.T) {
		ops := registry.ChangeSet{provOp(registry.EntryUpdate, a, nil)}
		annotateChangeSet(ops, from, to)
		require.NotNil(t, ops[0].Provenance)
		require.NotNil(t, ops[0].OriginalProvenance)
		assert.Equal(t, "2.0.0", ops[0].Provenance.Version)
		assert.Equal(t, "1.0.0", ops[0].OriginalProvenance.Version)
	})

	t.Run("delete takes its effective provenance from the source map", func(t *testing.T) {
		ops := registry.ChangeSet{provOp(registry.EntryDelete, a, nil)}
		annotateChangeSet(ops, from, registry.ProvMap{})
		require.NotNil(t, ops[0].Provenance)
		assert.Equal(t, "1.0.0", ops[0].Provenance.Version)
	})

	t.Run("directive-supplied provenance is kept", func(t *testing.T) {
		supplied := &registry.EntryProvenance{Module: "org/other"}
		ops := registry.ChangeSet{provOp(registry.EntryUpdate, a, supplied)}
		annotateChangeSet(ops, from, to)
		assert.Same(t, supplied, ops[0].Provenance)
		require.NotNil(t, ops[0].OriginalProvenance)
	})
}

func TestProvenanceForState(t *testing.T) {
	a := registry.NewID("ns", "a")
	b := registry.NewID("ns", "b")
	state := registry.State{{ID: a, Kind: "test"}, {ID: b, Kind: "test"}}
	prev := registry.ProvMap{a: {Module: "org/mod"}}
	ops := registry.ChangeSet{provOp(registry.EntryCreate, b, &registry.EntryProvenance{Module: "org/new"})}

	out, err := provenanceForState(state, prev, ops)
	require.NoError(t, err)
	assert.Equal(t, "org/mod", out[a].Module)
	assert.Equal(t, "org/new", out[b].Module)
	assert.Len(t, out, 2)
}

// TestApplyPublishesProvenanceWithState pins that the reader surface serves the
// records the applied changeset established, atomically with the state.
func TestApplyPublishesProvenanceWithState(t *testing.T) {
	reg, runner := newTestRegistry(t)
	id := registry.NewID("ns", "svc")
	entry := registry.Entry{ID: id, Kind: "test", Data: payloadNew("v1")}
	runner.RunFunc = func(_ registry.State, cs registry.ChangeSet) (registry.State, error) {
		out := registry.State{}
		for _, op := range cs {
			out = append(out, op.Entry)
		}
		return out, nil
	}

	_, err := reg.Apply(t.Context(), registry.ChangeSet{{
		Kind:       registry.EntryCreate,
		Entry:      entry,
		Provenance: &registry.EntryProvenance{Module: "org/mod", Version: "1.0.0", Digest: "sha256:x", Root: false},
	}})
	require.NoError(t, err)

	p, ok := reg.EntryProvenance(id)
	require.True(t, ok)
	assert.Equal(t, "org/mod", p.Module)
	assert.Equal(t, "1.0.0", p.Version)

	resident, err := reg.ResidentModules()
	require.NoError(t, err)
	require.Contains(t, resident, "org/mod")
	assert.Equal(t, "sha256:x", resident["org/mod"].Digest)
}

// TestUserUpdatePreservesProvenance pins the echo-back rule: a user operation
// carrying no provenance leaves ownership untouched.
func TestUserUpdatePreservesProvenance(t *testing.T) {
	reg, runner := newTestRegistry(t)
	id := registry.NewID("ns", "svc")
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

	_, err := reg.Apply(t.Context(), registry.ChangeSet{{
		Kind:       registry.EntryCreate,
		Entry:      registry.Entry{ID: id, Kind: "test", Data: payloadNew("v1")},
		Provenance: &registry.EntryProvenance{Module: "org/mod", Version: "1.0.0"},
	}})
	require.NoError(t, err)

	_, err = reg.Apply(t.Context(), registry.ChangeSet{{
		Kind:  registry.EntryUpdate,
		Entry: registry.Entry{ID: id, Kind: "test", Data: payloadNew("v2")},
	}})
	require.NoError(t, err)

	p, ok := reg.EntryProvenance(id)
	require.True(t, ok)
	assert.Equal(t, "org/mod", p.Module, "user echo-back must not strip ownership")

	_, err = reg.Apply(t.Context(), registry.ChangeSet{{
		Kind:  registry.EntryDelete,
		Entry: registry.Entry{ID: id, Kind: "test"},
	}})
	require.NoError(t, err)
	_, ok = reg.EntryProvenance(id)
	assert.False(t, ok, "delete clears the record")
}

func TestSnapshotStateDoesNotExposeLiveProvenance(t *testing.T) {
	reg, _ := newTestRegistry(t)
	id := registry.NewID("ns", "svc")
	reg.publishProvenance(registry.ProvMap{id: {Module: "org/mod", Version: "1.0.0"}})

	_, snapshot, err := reg.SnapshotState()
	require.NoError(t, err)
	snapshot.Prov[id] = registry.EntryProvenance{}

	p, ok := reg.EntryProvenance(id)
	require.True(t, ok)
	assert.Equal(t, "org/mod", p.Module)
}

func TestPublishProvenanceTakesOwnership(t *testing.T) {
	reg, _ := newTestRegistry(t)
	id := registry.NewID("ns", "svc")
	input := registry.ProvMap{id: {Module: "org/mod", Version: "1.0.0"}}
	reg.publishProvenance(input)
	input[id] = registry.EntryProvenance{}

	p, ok := reg.EntryProvenance(id)
	require.True(t, ok)
	assert.Equal(t, "org/mod", p.Module)
}

func TestResidentModulesRejectsConflictingIdentity(t *testing.T) {
	reg, _ := newTestRegistry(t)
	reg.publishProvenance(registry.ProvMap{
		registry.NewID("ns", "a"): {Module: "org/mod", Version: "1.0.0", Digest: "sha256:a"},
		registry.NewID("ns", "b"): {Module: "org/mod", Version: "2.0.0", Digest: "sha256:b"},
	})

	_, err := reg.ResidentModules()
	require.ErrorIs(t, err, registry.ErrConflictingModuleProvenance)
}
