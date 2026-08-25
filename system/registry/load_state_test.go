// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
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

// Alias for history package
var history = struct {
	NewMemory func() *historymem.Storage
}{
	NewMemory: historymem.New,
}

// hostProvenanced wraps a raw test state with host provenance records.
func hostProvenanced(s registry.State) registry.ProvenancedState {
	prov := make(registry.ProvMap, len(s))
	for _, e := range s {
		prov[e.ID] = registry.EntryProvenance{}
	}
	return registry.ProvenancedState{Entries: s, Prov: prov}
}

func TestRegistry_LoadState_V0(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	hist := history.NewMemory()

	mockRunner := NewMockRunner()
	mockRunner.RunFunc = func(state registry.State, changes registry.ChangeSet) (registry.State, error) {
		stateMap := make(map[registry.ID]registry.Entry)
		for _, e := range state {
			stateMap[e.ID] = e
		}
		for _, op := range changes {
			switch op.Kind {
			case registry.EntryCreate, registry.EntryUpdate:
				stateMap[op.Entry.ID] = op.Entry
			case registry.EntryDelete:
				delete(stateMap, op.Entry.ID)
			}
		}
		result := make(registry.State, 0, len(stateMap))
		for _, e := range stateMap {
			result = append(result, e)
		}
		return result, nil
	}

	resolver := topology.NewResolver()
	reg := NewRegistry(
		hist,
		mockRunner,
		topology.NewStateBuilder(logger, resolver),
		resolver,
		logger,
	)

	baseline := registry.State{
		{ID: registry.NewID("test", "entry1"), Kind: "service"},
		{ID: registry.NewID("test", "entry2"), Kind: "service"},
	}

	// When history is empty, Head() returns error, so use Current() as fallback
	head, err := hist.Head()
	if err != nil {
		head, err = reg.Current()
		require.NoError(t, err)
	}
	require.Equal(t, uint(0), head.ID())

	err = reg.LoadState(ctx, hostProvenanced(baseline), head)
	require.NoError(t, err)

	currentVer, err := reg.Current()
	require.NoError(t, err)
	assert.Equal(t, uint(0), currentVer.ID())

	assert.Len(t, reg.state, 2)
}

func TestRegistry_LoadState_V0ExpandsBaselineDirectives(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	hist := history.NewMemory()
	resolver := topology.NewResolver()

	depEntry := registry.Entry{
		ID:   registry.NewID("app.deps", "analysis"),
		Kind: registry.NamespaceDependency,
		Data: payload.New("dependency"),
	}
	expandedEntry := registry.Entry{
		ID:   registry.NewID("analysis", "runtime"),
		Kind: "process.host",
		Data: payload.New("expanded"),
	}

	expander := directiveFunc(func(_ context.Context, op registry.Operation, snapshot registry.ProvenancedState) (registry.DirectiveResult, error) {
		require.Equal(t, registry.EntryUpdate, op.Kind)
		require.Equal(t, depEntry.ID, op.Entry.ID)
		require.Contains(t, snapshot.Entries, depEntry)
		return registry.DirectiveResult{
			Applied: true,
			Additional: []registry.ScopedOperation{{
				Operation: registry.Operation{Kind: registry.EntryCreate, Entry: expandedEntry},
				Scope:     registry.ScopeBaseline,
			}},
		}, nil
	})

	runner := NewMockRunner()
	runner.RunFunc = func(state registry.State, changes registry.ChangeSet) (registry.State, error) {
		stateMap := topology.NewStateMap(state)
		for _, op := range changes {
			switch op.Kind {
			case registry.EntryCreate, registry.EntryUpdate:
				stateMap[op.Entry.ID] = op.Entry
			case registry.EntryDelete:
				delete(stateMap, op.Entry.ID)
			}
		}
		return topology.StateMapToSlice(stateMap), nil
	}

	reg := NewRegistry(
		hist,
		runner,
		topology.NewStateBuilder(logger, resolver),
		resolver,
		logger,
		WithKindDirective(registry.NamespaceDependency, expander),
	)

	head, err := reg.Current()
	require.NoError(t, err)
	require.NoError(t, reg.LoadState(ctx, hostProvenanced(registry.State{depEntry}), head))

	_, err = reg.GetEntry(depEntry.ID)
	require.NoError(t, err)
	_, err = reg.GetEntry(expandedEntry.ID)
	require.NoError(t, err)
}

func TestRegistry_LoadState_V0RejectsBaselineDirectiveExpansionFailure(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	resolver := topology.NewResolver()
	depEntry := registry.Entry{
		ID:   registry.NewID("app.deps", "missing"),
		Kind: registry.NamespaceDependency,
	}

	runner := NewMockRunner()
	runner.RunFunc = func(state registry.State, _ registry.ChangeSet) (registry.State, error) {
		return state, nil
	}
	reg := NewRegistry(
		history.NewMemory(),
		runner,
		topology.NewStateBuilder(logger, resolver),
		resolver,
		logger,
		WithKindDirective(registry.NamespaceDependency, directiveFunc(
			func(context.Context, registry.Operation, registry.ProvenancedState) (registry.DirectiveResult, error) {
				return registry.DirectiveResult{}, assert.AnError
			},
		)),
	)

	head, err := reg.Current()
	require.NoError(t, err)
	err = reg.LoadState(ctx, hostProvenanced(registry.State{depEntry}), head)
	require.ErrorIs(t, err, assert.AnError)
	_, err = reg.GetEntry(depEntry.ID)
	require.Error(t, err, "a failed declaration must not be published as a partial root")
}

func TestRegistry_LoadState_WithHistory(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	hist := history.NewMemory()

	mockRunner := NewMockRunner()
	mockRunner.RunFunc = func(state registry.State, changes registry.ChangeSet) (registry.State, error) {
		stateMap := make(map[registry.ID]registry.Entry)
		for _, e := range state {
			stateMap[e.ID] = e
		}
		for _, op := range changes {
			switch op.Kind {
			case registry.EntryCreate, registry.EntryUpdate:
				stateMap[op.Entry.ID] = op.Entry
			case registry.EntryDelete:
				delete(stateMap, op.Entry.ID)
			}
		}
		result := make(registry.State, 0, len(stateMap))
		for _, e := range stateMap {
			result = append(result, e)
		}
		return result, nil
	}

	resolver := topology.NewResolver()
	reg := NewRegistry(
		hist,
		mockRunner,
		topology.NewStateBuilder(logger, resolver),
		resolver,
		logger,
	)

	baseline := registry.State{
		{ID: registry.NewID("test", "entry1"), Kind: "service", Data: payload.New("initial")},
		{ID: registry.NewID("test", "entry2"), Kind: "service", Data: payload.New("initial")},
	}
	require.NoError(t, reg.LoadState(ctx, hostProvenanced(baseline), version.New(registry.RootVersion)))

	cs1 := registry.ChangeSet{
		{Kind: registry.EntryUpdate, Entry: registry.Entry{ID: registry.NewID("test", "entry1"), Kind: "service", Data: payload.New("updated")}},
		{Kind: registry.EntryCreate, Entry: registry.Entry{ID: registry.NewID("test", "entry3"), Kind: "service", Data: payload.New("new")}},
	}

	v1, err := reg.Apply(ctx, cs1)
	require.NoError(t, err)
	assert.Equal(t, uint(1), v1.ID())

	mockRunner2 := NewMockRunner()
	mockRunner2.RunFunc = mockRunner.RunFunc

	reg2 := NewRegistry(
		hist,
		mockRunner2,
		topology.NewStateBuilder(logger, resolver),
		resolver,
		logger,
	)

	head, err := hist.Head()
	require.NoError(t, err)
	assert.Equal(t, uint(1), head.ID())

	err = reg2.LoadState(ctx, hostProvenanced(baseline), head)
	require.NoError(t, err)

	currentVer, err := reg2.Current()
	require.NoError(t, err)
	assert.Equal(t, uint(1), currentVer.ID())

	assert.Len(t, reg2.state, 3)

	found := false
	for _, e := range reg2.state {
		if e.ID.NS == "test" && e.ID.Name == "entry1" {
			assert.Equal(t, "updated", e.Data.Data())
			found = true
		}
	}
	assert.True(t, found, "entry1 should be updated")
}

func TestRegistry_LoadState_ReplaysHistoryThroughDirectives(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	hist := history.NewMemory()
	resolver := topology.NewResolver()

	run := func(state registry.State, changes registry.ChangeSet) (registry.State, error) {
		stateMap := make(map[registry.ID]registry.Entry)
		for _, e := range state {
			stateMap[e.ID] = e
		}
		for _, op := range changes {
			switch op.Kind {
			case registry.EntryCreate, registry.EntryUpdate:
				stateMap[op.Entry.ID] = op.Entry
			case registry.EntryDelete:
				delete(stateMap, op.Entry.ID)
			}
		}
		result := make(registry.State, 0, len(stateMap))
		for _, e := range stateMap {
			result = append(result, e)
		}
		return result, nil
	}

	depEntry := registry.Entry{
		ID:   registry.NewID("app.deps", "sso"),
		Kind: registry.NamespaceDependency,
		Data: payload.New("dep"),
	}
	expandedEntry := registry.Entry{
		ID:   registry.NewID("kickside.sso", "flow_store"),
		Kind: "store.memory",
		Data: payload.New("expanded"),
	}
	expander := directiveFunc(func(_ context.Context, op registry.Operation, _ registry.ProvenancedState) (registry.DirectiveResult, error) {
		if op.Entry.Kind != registry.NamespaceDependency {
			return registry.DirectiveResult{}, nil
		}
		return registry.DirectiveResult{
			Applied: true,
			Additional: []registry.ScopedOperation{{
				Operation: registry.Operation{Kind: registry.EntryCreate, Entry: expandedEntry},
				Scope:     registry.ScopeBaseline,
			}},
		}, nil
	})

	runner := NewMockRunner()
	runner.RunFunc = run
	reg := NewRegistry(
		hist,
		runner,
		topology.NewStateBuilder(logger, resolver),
		resolver,
		logger,
		WithKindDirective(registry.NamespaceDependency, expander),
	)

	v1, err := reg.Apply(ctx, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: depEntry}})
	require.NoError(t, err)
	require.Equal(t, uint(1), v1.ID())

	stored, err := hist.Get(v1)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.Equal(t, depEntry.ID, stored[0].Entry.ID)

	runner2 := NewMockRunner()
	runner2.RunFunc = run
	reg2 := NewRegistry(
		hist,
		runner2,
		topology.NewStateBuilder(logger, resolver),
		resolver,
		logger,
		WithKindDirective(registry.NamespaceDependency, expander),
	)

	head, err := hist.Head()
	require.NoError(t, err)
	require.NoError(t, reg2.LoadState(ctx, hostProvenanced(nil), head))

	_, err = reg2.GetEntry(depEntry.ID)
	require.NoError(t, err)
	_, err = reg2.GetEntry(expandedEntry.ID)
	require.NoError(t, err)
}

func TestRegistry_LoadState_MultipleVersions(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	hist := history.NewMemory()

	mockRunner := NewMockRunner()
	mockRunner.RunFunc = func(state registry.State, changes registry.ChangeSet) (registry.State, error) {
		stateMap := make(map[registry.ID]registry.Entry)
		for _, e := range state {
			stateMap[e.ID] = e
		}
		for _, op := range changes {
			switch op.Kind {
			case registry.EntryCreate, registry.EntryUpdate:
				stateMap[op.Entry.ID] = op.Entry
			case registry.EntryDelete:
				delete(stateMap, op.Entry.ID)
			}
		}
		result := make(registry.State, 0, len(stateMap))
		for _, e := range stateMap {
			result = append(result, e)
		}
		return result, nil
	}

	resolver := topology.NewResolver()
	reg := NewRegistry(
		hist,
		mockRunner,
		topology.NewStateBuilder(logger, resolver),
		resolver,
		logger,
	)

	baseline := registry.State{
		{ID: registry.NewID("test", "entry1"), Kind: "service", Data: payload.New("v0")},
	}
	require.NoError(t, reg.LoadState(ctx, hostProvenanced(baseline), version.New(registry.RootVersion)))

	cs1 := registry.ChangeSet{
		{Kind: registry.EntryUpdate, Entry: registry.Entry{ID: registry.NewID("test", "entry1"), Kind: "service", Data: payload.New("v1")}},
	}
	_, err := reg.Apply(ctx, cs1)
	require.NoError(t, err)

	cs2 := registry.ChangeSet{
		{Kind: registry.EntryUpdate, Entry: registry.Entry{ID: registry.NewID("test", "entry1"), Kind: "service", Data: payload.New("v2")}},
	}
	_, err = reg.Apply(ctx, cs2)
	require.NoError(t, err)

	cs3 := registry.ChangeSet{
		{Kind: registry.EntryUpdate, Entry: registry.Entry{ID: registry.NewID("test", "entry1"), Kind: "service", Data: payload.New("v3")}},
	}
	v3, err := reg.Apply(ctx, cs3)
	require.NoError(t, err)

	mockRunner2 := NewMockRunner()
	mockRunner2.RunFunc = mockRunner.RunFunc

	reg2 := NewRegistry(
		hist,
		mockRunner2,
		topology.NewStateBuilder(logger, resolver),
		resolver,
		logger,
	)

	err = reg2.LoadState(ctx, hostProvenanced(baseline), v3)
	require.NoError(t, err)

	currentVer, err := reg2.Current()
	require.NoError(t, err)
	assert.Equal(t, uint(3), currentVer.ID())

	found := false
	for _, e := range reg2.state {
		if e.ID.NS == "test" && e.ID.Name == "entry1" {
			assert.Equal(t, "v3", e.Data.Data())
			found = true
		}
	}
	assert.True(t, found, "entry1 should have v3 value")
}

func TestRegistry_LoadState_ThenApplyVersion(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	hist := history.NewMemory()

	mockRunner := NewMockRunner()
	mockRunner.RunFunc = func(state registry.State, changes registry.ChangeSet) (registry.State, error) {
		stateMap := make(map[registry.ID]registry.Entry)
		for _, e := range state {
			stateMap[e.ID] = e
		}
		// Log for the first registry (during setup)
		for _, op := range changes {
			switch op.Kind {
			case registry.EntryCreate, registry.EntryUpdate:
				stateMap[op.Entry.ID] = op.Entry
			case registry.EntryDelete:
				delete(stateMap, op.Entry.ID)
			}
		}
		result := make(registry.State, 0, len(stateMap))
		for _, e := range stateMap {
			result = append(result, e)
		}
		return result, nil
	}

	resolver := topology.NewResolver()
	reg := NewRegistry(
		hist,
		mockRunner,
		topology.NewStateBuilder(logger, resolver),
		resolver,
		logger,
	)

	baseline := registry.State{
		{ID: registry.NewID("test", "entry1"), Kind: "service", Data: payload.New("v0")},
	}

	// Create initial version with baseline
	v1, err := reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: baseline[0]},
	})
	require.NoError(t, err)
	require.Equal(t, uint(1), v1.ID())

	cs2 := registry.ChangeSet{
		{Kind: registry.EntryUpdate, Entry: registry.Entry{ID: registry.NewID("test", "entry1"), Kind: "service", Data: payload.New("v2")}},
	}
	v2, err := reg.Apply(ctx, cs2)
	require.NoError(t, err)
	require.Equal(t, uint(2), v2.ID())

	cs3 := registry.ChangeSet{
		{Kind: registry.EntryUpdate, Entry: registry.Entry{ID: registry.NewID("test", "entry1"), Kind: "service", Data: payload.New("v3")}},
	}
	v3, err := reg.Apply(ctx, cs3)
	require.NoError(t, err)
	require.Equal(t, uint(3), v3.ID())

	mockRunner2 := NewMockRunner()
	mockRunner2.RunFunc = mockRunner.RunFunc

	reg2 := NewRegistry(
		hist,
		mockRunner2,
		topology.NewStateBuilder(logger, resolver),
		resolver,
		logger,
	)

	// Get head from history (should be v3)
	head, err := hist.Head()
	require.NoError(t, err)
	require.Equal(t, uint(3), head.ID())

	err = reg2.LoadState(ctx, hostProvenanced(baseline), head)
	require.NoError(t, err)

	currentVer, err := reg2.Current()
	require.NoError(t, err)
	assert.Equal(t, uint(3), currentVer.ID())

	// Verify state at v3
	found := false
	for _, e := range reg2.state {
		if e.ID.NS == "test" && e.ID.Name == "entry1" {
			assert.Equal(t, "v3", e.Data.Data())
			found = true
		}
	}
	assert.True(t, found, "entry1 should have v3 value")

	// Apply v2 (rollback) - using v2 from first registry
	// The fix should handle this by looking up version by ID
	err = reg2.ApplyVersion(ctx, v2)
	require.NoError(t, err)

	currentVer, err = reg2.Current()
	require.NoError(t, err)
	assert.Equal(t, uint(2), currentVer.ID())

	// Verify state at v2
	found = false
	for _, e := range reg2.state {
		if e.ID.NS == "test" && e.ID.Name == "entry1" {
			assert.Equal(t, "v2", e.Data.Data())
			found = true
		}
	}
	assert.True(t, found, "entry1 should have v2 value after rollback")
}
