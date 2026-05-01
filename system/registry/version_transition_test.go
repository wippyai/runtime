// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"fmt"
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

// TestRunner implements registry.Runner for testing with transition tracking
type TestRunner struct {
	transitions []registry.ChangeSet
	state       registry.State
}

func NewTestRunner() *TestRunner {
	return &TestRunner{
		transitions: []registry.ChangeSet{},
		state:       registry.State{},
	}
}

func (m *TestRunner) Transition(_ context.Context, from registry.State, cs registry.ChangeSet) (registry.State, error) {
	m.transitions = append(m.transitions, cs)

	// Apply the changeset to state
	stateMap := make(map[registry.ID]registry.Entry, len(from))
	for _, entry := range from {
		stateMap[entry.ID] = entry
	}

	for _, op := range cs {
		switch op.Kind {
		case registry.EntryCreate:
			stateMap[op.Entry.ID] = op.Entry
		case registry.EntryUpdate:
			stateMap[op.Entry.ID] = op.Entry
		case registry.EntryDelete:
			delete(stateMap, op.Entry.ID)
		}
	}

	result := make(registry.State, 0, len(stateMap))
	for _, entry := range stateMap {
		result = append(result, entry)
	}

	m.state = result
	return result, nil
}

func (m *TestRunner) LastTransition() registry.ChangeSet {
	if len(m.transitions) > 0 {
		return m.transitions[len(m.transitions)-1]
	}
	return nil
}

func (m *TestRunner) TransitionCount() int {
	return len(m.transitions)
}

type directiveFunc func(context.Context, registry.Operation, registry.State) (registry.DirectiveResult, error)

func (f directiveFunc) Expand(ctx context.Context, op registry.Operation, snap registry.State) (registry.DirectiveResult, error) {
	return f(ctx, op, snap)
}

func TestApplyVersion_ForwardWithSquashing(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	// Setup
	hist := historymem.New()
	runner := NewTestRunner()
	builder := topology.NewStateBuilder(logger, nil)
	reg := NewRegistry(hist, runner, builder, nil, logger)

	// Create baseline entries
	baseline := registry.State{
		{
			ID:   registry.NewID("base", "config"),
			Kind: "config",
			Data: payload.NewString("base-config"),
		},
	}

	// Load baseline at v0
	err := reg.LoadState(ctx, baseline, version.FromParent(nil, 0))
	require.NoError(t, err)

	// Create test entry that will be modified multiple times
	entryID := registry.NewID("test", "entry1")

	// v1: Create entry
	_, err = reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: registry.Entry{
			ID:   entryID,
			Kind: "service",
			Data: payload.NewString("v1"),
		}},
	})
	require.NoError(t, err)

	// v2: Update entry
	_, err = reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryUpdate, Entry: registry.Entry{
			ID:   entryID,
			Kind: "service",
			Data: payload.NewString("v2"),
		}},
	})
	require.NoError(t, err)

	// v3: Update entry again
	v3, err := reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryUpdate, Entry: registry.Entry{
			ID:   entryID,
			Kind: "service",
			Data: payload.NewString("v3"),
		}},
	})
	require.NoError(t, err)

	// Reset to v0 (baseline only)
	err = reg.ApplyVersion(ctx, version.FromParent(nil, 0))
	require.NoError(t, err)

	// Clear transition history
	runner.transitions = []registry.ChangeSet{}

	// Jump directly from v0 to v3
	err = reg.ApplyVersion(ctx, v3)
	require.NoError(t, err)

	// Check that only one transition was made (squashed)
	assert.Equal(t, 1, runner.TransitionCount(), "Should have made only one transition with squashed changeset")

	// Check the squashed changeset
	lastTransition := runner.LastTransition()
	require.NotNil(t, lastTransition)

	// Should have only one operation: Create with v3 data
	// (Create + Update + Update = Create with final value)
	assert.Len(t, lastTransition, 1, "Squashed changeset should have one operation")
	assert.Equal(t, registry.EntryCreate, lastTransition[0].Kind)
	assert.Equal(t, "v3", lastTransition[0].Entry.Data.Data().(string))

	// Verify final state
	entries, err := reg.GetAllEntries()
	require.NoError(t, err)
	assert.Len(t, entries, 2) // baseline + test entry

	// Find test entry
	var found bool
	for _, e := range entries {
		if e.ID == entryID {
			found = true
			assert.Equal(t, "v3", e.Data.Data().(string))
		}
	}
	assert.True(t, found, "Test entry should exist in final state")
}

func TestApplyVersion_BackwardWithSquashing(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	// Setup
	hist := historymem.New()
	runner := NewTestRunner()
	builder := topology.NewStateBuilder(logger, nil)
	reg := NewRegistry(hist, runner, builder, nil, logger)

	// Create baseline
	baseline := registry.State{
		{
			ID:   registry.NewID("base", "config"),
			Kind: "config",
			Data: payload.NewString("base-config"),
		},
	}

	// Load baseline at v0
	err := reg.LoadState(ctx, baseline, version.FromParent(nil, 0))
	require.NoError(t, err)

	// Create entries across versions
	entry1ID := registry.NewID("test", "entry1")
	entry2ID := registry.NewID("test", "entry2")
	entry3ID := registry.NewID("test", "entry3")

	// v1: Create entry1
	v1, err := reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: registry.Entry{
			ID:   entry1ID,
			Kind: "service",
			Data: payload.NewString("entry1-v1"),
		}},
	})
	require.NoError(t, err)

	// v2: Create entry2, update entry1
	_, err = reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: registry.Entry{
			ID:   entry2ID,
			Kind: "service",
			Data: payload.NewString("entry2-v2"),
		}},
		{Kind: registry.EntryUpdate, Entry: registry.Entry{
			ID:   entry1ID,
			Kind: "service",
			Data: payload.NewString("entry1-v2"),
		}},
	})
	require.NoError(t, err)

	// v3: Create entry3, delete entry2
	_, err = reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: registry.Entry{
			ID:   entry3ID,
			Kind: "service",
			Data: payload.NewString("entry3-v3"),
		}},
		{Kind: registry.EntryDelete, Entry: registry.Entry{
			ID:   entry2ID,
			Kind: "service",
		}},
	})
	require.NoError(t, err)

	// Now at v3, roll back to v1
	runner.transitions = []registry.ChangeSet{}
	err = reg.ApplyVersion(ctx, v1)
	require.NoError(t, err)

	// Should have made one transition with squashed reversed changesets
	assert.Equal(t, 1, runner.TransitionCount(), "Should have made only one transition")

	// Check the reversed/squashed changeset
	lastTransition := runner.LastTransition()
	require.NotNil(t, lastTransition)

	// Should have operations to:
	// - Delete entry3 (reverse of Create in v3)
	// - Create entry2 (reverse of Delete in v3)
	// - Update entry1 back to v1 (reverse of Update in v2)
	// But squashed efficiently

	// Verify final state matches v1
	entries, err := reg.GetAllEntries()
	require.NoError(t, err)
	assert.Len(t, entries, 2) // baseline + entry1

	for _, e := range entries {
		switch e.ID {
		case entry1ID:
			assert.Equal(t, "entry1-v1", e.Data.Data().(string))
		case entry2ID:
			t.Error("entry2 should not exist at v1")
		case entry3ID:
			t.Error("entry3 should not exist at v1")
		}
	}
}

func TestApplyVersion_BackwardDeletesDependentsBeforeDependencies(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	hist := historymem.New()
	runner := NewTestRunner()
	builder := topology.NewStateBuilder(logger, nil)
	reg := NewRegistry(hist, runner, builder, nil, logger)

	baseline := registry.State{
		{
			ID:   registry.NewID("base", "config"),
			Kind: "config",
			Data: payload.NewString("base-config"),
		},
	}
	v0 := version.FromParent(nil, 0)
	err := reg.LoadState(ctx, baseline, v0)
	require.NoError(t, err)

	repoID := registry.NewID("app", "repo")
	handlerID := registry.NewID("app", "handler")
	repo := registry.Entry{
		ID:   repoID,
		Kind: "library.lua",
		Meta: map[string]any{},
	}
	handler := registry.Entry{
		ID:   handlerID,
		Kind: "function.lua",
		Meta: map[string]any{
			registry.TagDependsOn: []string{repoID.String()},
		},
	}

	_, err = reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: repo},
		{Kind: registry.EntryCreate, Entry: handler},
	})
	require.NoError(t, err)

	runner.transitions = []registry.ChangeSet{}
	err = reg.ApplyVersion(ctx, v0)
	require.NoError(t, err)

	require.Equal(t, 1, runner.TransitionCount())
	lastTransition := runner.LastTransition()
	require.Len(t, lastTransition, 2)
	assert.Equal(t, registry.EntryDelete, lastTransition[0].Kind)
	assert.Equal(t, handlerID, lastTransition[0].Entry.ID)
	assert.Equal(t, registry.EntryDelete, lastTransition[1].Kind)
	assert.Equal(t, repoID, lastTransition[1].Entry.ID)

	_, err = reg.GetEntry(repoID)
	require.Error(t, err)
	_, err = reg.GetEntry(handlerID)
	require.Error(t, err)
}

func TestApplyVersion_CrossBranchWithSquashing(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	// Setup
	hist := historymem.New()
	runner := NewTestRunner()
	builder := topology.NewStateBuilder(logger, nil)
	reg := NewRegistry(hist, runner, builder, nil, logger)

	// Create baseline
	baseline := registry.State{
		{
			ID:   registry.NewID("base", "config"),
			Kind: "config",
			Data: payload.NewString("base-config"),
		},
	}

	// Load baseline at v0
	err := reg.LoadState(ctx, baseline, version.FromParent(nil, 0))
	require.NoError(t, err)

	entryID := registry.NewID("test", "shared")

	// v1: Create shared entry
	v1, err := reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: registry.Entry{
			ID:   entryID,
			Kind: "service",
			Data: payload.NewString("v1"),
		}},
	})
	require.NoError(t, err)

	// Branch 1: v2 updates shared entry
	_, err = reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryUpdate, Entry: registry.Entry{
			ID:   entryID,
			Kind: "service",
			Data: payload.NewString("branch1-v2"),
		}},
	})
	require.NoError(t, err)

	// Branch 1: v3 updates again
	v3, err := reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryUpdate, Entry: registry.Entry{
			ID:   entryID,
			Kind: "service",
			Data: payload.NewString("branch1-v3"),
		}},
	})
	require.NoError(t, err)

	// Switch back to v1 to create another branch
	err = reg.ApplyVersion(ctx, v1)
	require.NoError(t, err)

	// Branch 2: v4 (from v1) updates differently
	_, err = reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryUpdate, Entry: registry.Entry{
			ID:   entryID,
			Kind: "service",
			Data: payload.NewString("branch2-v4"),
		}},
	})
	require.NoError(t, err)

	// Now jump from v4 (branch 2) to v3 (branch 1)
	// This should go v4 -> v1 -> v3
	runner.transitions = []registry.ChangeSet{}
	err = reg.ApplyVersion(ctx, v3)
	require.NoError(t, err)

	// Should have made one squashed transition
	assert.Equal(t, 1, runner.TransitionCount(), "Should have made only one transition")

	// Verify final state
	entries, err := reg.GetAllEntries()
	require.NoError(t, err)

	var found bool
	for _, e := range entries {
		if e.ID == entryID {
			found = true
			assert.Equal(t, "branch1-v3", e.Data.Data().(string))
		}
	}
	assert.True(t, found, "Shared entry should have branch1-v3 value")
}

func TestApplyVersion_PreservesBaseline(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	// Setup with SQLite to test the original bug scenario
	hist := historymem.New()
	runner := NewTestRunner()
	builder := topology.NewStateBuilder(logger, nil)
	reg := NewRegistry(hist, runner, builder, nil, logger)

	// Create substantial baseline (simulating lockfile entries)
	baseline := registry.State{}
	for i := 0; i < 10; i++ {
		baseline = append(baseline, registry.Entry{
			ID:   registry.ID{NS: "baseline", Name: fmt.Sprintf("entry-%d", i)},
			Kind: "service",
			Data: payload.NewString(fmt.Sprintf("baseline-data-%d", i)),
		})
	}

	// Load baseline at v0
	err := reg.LoadState(ctx, baseline, version.FromParent(nil, 0))
	require.NoError(t, err)

	// v1: Add one new entry
	newEntry := registry.Entry{
		ID:   registry.NewID("app", "feature1"),
		Kind: "component",
		Data: payload.NewString("feature1-data"),
	}
	v1, err := reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: newEntry},
	})
	require.NoError(t, err)

	// v2: Update the new entry
	_, err = reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryUpdate, Entry: registry.Entry{
			ID:   newEntry.ID,
			Kind: "component",
			Data: payload.NewString("feature1-updated"),
		}},
	})
	require.NoError(t, err)

	// Roll back to v1
	err = reg.ApplyVersion(ctx, v1)
	require.NoError(t, err)

	// Critical check: Ensure baseline entries are still present
	entries, err := reg.GetAllEntries()
	require.NoError(t, err)

	// Should have all baseline entries plus the one from v1
	assert.Len(t, entries, 11, "Should have 10 baseline + 1 new entry")

	// Count baseline entries
	baselineCount := 0
	for _, e := range entries {
		if e.ID.NS == "baseline" {
			baselineCount++
		}
	}
	assert.Equal(t, 10, baselineCount, "All baseline entries should be preserved")

	// Check the new entry has v1 data
	for _, e := range entries {
		if e.ID.NS == "app" && e.ID.Name == "feature1" {
			assert.Equal(t, "feature1-data", e.Data.Data().(string))
		}
	}
}

func TestApplyVersion_RunsDirectives(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	hist := historymem.New()
	runner := NewTestRunner()
	resolver := topology.NewResolver()
	builder := topology.NewStateBuilder(logger, resolver)

	depID := registry.NewID("app", "dep")
	modID := registry.NewID("mod", "svc")

	expander := directiveFunc(func(_ context.Context, op registry.Operation, _ registry.State) (registry.DirectiveResult, error) {
		if op.Entry.Kind != registry.NamespaceDependency {
			return registry.DirectiveResult{}, nil
		}
		val := ""
		if op.Entry.Data != nil {
			if data, ok := op.Entry.Data.Data().(map[string]any); ok {
				if v, ok := data["value"].(string); ok {
					val = v
				}
			}
		}
		kind := registry.EntryUpdate
		if op.Kind == registry.EntryCreate {
			kind = registry.EntryCreate
		}
		modEntry := registry.Entry{
			ID:   modID,
			Kind: "service",
			Data: payload.NewString(val),
		}
		return registry.DirectiveResult{
			Applied: true,
			Additional: []registry.ScopedOperation{
				{
					Operation: registry.Operation{Kind: kind, Entry: modEntry},
					Scope:     registry.ScopeBaseline,
				},
			},
		}, nil
	})

	reg := NewRegistry(hist, runner, builder, resolver, logger,
		WithKindDirective(registry.NamespaceDependency, expander),
	)

	v1, err := reg.Apply(ctx, registry.ChangeSet{
		{
			Kind: registry.EntryCreate,
			Entry: registry.Entry{
				ID:   depID,
				Kind: registry.NamespaceDependency,
				Data: payload.New(map[string]any{"value": "v1"}),
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, v1)

	entry, err := reg.GetEntry(modID)
	require.NoError(t, err)
	assert.Equal(t, "v1", entry.Data.Data().(string))

	_, err = reg.Apply(ctx, registry.ChangeSet{
		{
			Kind: registry.EntryUpdate,
			Entry: registry.Entry{
				ID:   depID,
				Kind: registry.NamespaceDependency,
				Data: payload.New(map[string]any{"value": "v2"}),
			},
		},
	})
	require.NoError(t, err)

	entry, err = reg.GetEntry(modID)
	require.NoError(t, err)
	assert.Equal(t, "v2", entry.Data.Data().(string))

	err = reg.ApplyVersion(ctx, v1)
	require.NoError(t, err)

	entry, err = reg.GetEntry(modID)
	require.NoError(t, err)
	assert.Equal(t, "v1", entry.Data.Data().(string))
}
