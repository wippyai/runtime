package registry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/registry/history"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

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
			case registry.Create, registry.Update:
				stateMap[op.Entry.ID] = op.Entry
			case registry.Delete:
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
		{ID: registry.ID{NS: "test", Name: "entry1"}, Kind: "service"},
		{ID: registry.ID{NS: "test", Name: "entry2"}, Kind: "service"},
	}

	// When history is empty, Head() returns error, so use Current() as fallback
	head, err := hist.Head()
	if err != nil {
		head, err = reg.Current()
		require.NoError(t, err)
	}
	require.Equal(t, uint(0), head.ID())

	err = reg.LoadState(ctx, baseline, head)
	require.NoError(t, err)

	currentVer, err := reg.Current()
	require.NoError(t, err)
	assert.Equal(t, uint(0), currentVer.ID())

	assert.Len(t, reg.state, 2)
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
			case registry.Create, registry.Update:
				stateMap[op.Entry.ID] = op.Entry
			case registry.Delete:
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
		{ID: registry.ID{NS: "test", Name: "entry1"}, Kind: "service", Data: payload.New("initial")},
		{ID: registry.ID{NS: "test", Name: "entry2"}, Kind: "service", Data: payload.New("initial")},
	}

	cs1 := registry.ChangeSet{
		{Kind: registry.Update, Entry: registry.Entry{ID: registry.ID{NS: "test", Name: "entry1"}, Kind: "service", Data: payload.New("updated")}},
		{Kind: registry.Create, Entry: registry.Entry{ID: registry.ID{NS: "test", Name: "entry3"}, Kind: "service", Data: payload.New("new")}},
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

	err = reg2.LoadState(ctx, baseline, head)
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
			case registry.Create, registry.Update:
				stateMap[op.Entry.ID] = op.Entry
			case registry.Delete:
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
		{ID: registry.ID{NS: "test", Name: "entry1"}, Kind: "service", Data: payload.New("v0")},
	}

	cs1 := registry.ChangeSet{
		{Kind: registry.Update, Entry: registry.Entry{ID: registry.ID{NS: "test", Name: "entry1"}, Kind: "service", Data: payload.New("v1")}},
	}
	_, err := reg.Apply(ctx, cs1)
	require.NoError(t, err)

	cs2 := registry.ChangeSet{
		{Kind: registry.Update, Entry: registry.Entry{ID: registry.ID{NS: "test", Name: "entry1"}, Kind: "service", Data: payload.New("v2")}},
	}
	_, err = reg.Apply(ctx, cs2)
	require.NoError(t, err)

	cs3 := registry.ChangeSet{
		{Kind: registry.Update, Entry: registry.Entry{ID: registry.ID{NS: "test", Name: "entry1"}, Kind: "service", Data: payload.New("v3")}},
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

	err = reg2.LoadState(ctx, baseline, v3)
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
			case registry.Create, registry.Update:
				stateMap[op.Entry.ID] = op.Entry
			case registry.Delete:
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
		{ID: registry.ID{NS: "test", Name: "entry1"}, Kind: "service", Data: payload.New("v0")},
	}

	// Create initial version with baseline
	v1, err := reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.Create, Entry: baseline[0]},
	})
	require.NoError(t, err)
	require.Equal(t, uint(1), v1.ID())

	cs2 := registry.ChangeSet{
		{Kind: registry.Update, Entry: registry.Entry{ID: registry.ID{NS: "test", Name: "entry1"}, Kind: "service", Data: payload.New("v2")}},
	}
	v2, err := reg.Apply(ctx, cs2)
	require.NoError(t, err)
	require.Equal(t, uint(2), v2.ID())

	cs3 := registry.ChangeSet{
		{Kind: registry.Update, Entry: registry.Entry{ID: registry.ID{NS: "test", Name: "entry1"}, Kind: "service", Data: payload.New("v3")}},
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

	err = reg2.LoadState(ctx, baseline, head)
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
