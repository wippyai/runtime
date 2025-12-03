package sqlite

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	registrysystem "github.com/wippyai/runtime/system/registry"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

// TestRunner implements registry.Runner for testing
type TestRunner struct{}

func (r *TestRunner) Transition(_ context.Context, from registry.State, cs registry.ChangeSet) (registry.State, error) {
	stateMap := make(map[registry.ID]registry.Entry, len(from))
	for _, entry := range from {
		stateMap[entry.ID] = entry
	}

	for _, op := range cs {
		switch op.Kind {
		case registry.Create:
			stateMap[op.Entry.ID] = op.Entry
		case registry.Update:
			stateMap[op.Entry.ID] = op.Entry
		case registry.Delete:
			delete(stateMap, op.Entry.ID)
		}
	}

	result := make(registry.State, 0, len(stateMap))
	for _, entry := range stateMap {
		result = append(result, entry)
	}
	return result, nil
}

func TestSQLitePersistence_OriginalEntry(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	// Create temp SQLite database
	tmpFile, err := os.CreateTemp("", "test-*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	// Create history with SQLite
	hist, err := NewSQLite(tmpFile.Name(), logger)
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	runner := &TestRunner{}
	builder := topology.NewStateBuilder(logger, nil)
	reg := registrysystem.NewRegistry(hist, runner, builder, nil, logger)

	// Load baseline at v0
	baseline := registry.State{
		{
			ID:   registry.NewID("base", "config"),
			Kind: "config",
			Data: payload.NewString("baseline"),
		},
	}
	err = reg.LoadState(ctx, baseline, version.FromParent(nil, 0))
	require.NoError(t, err)

	entryID := registry.NewID("test", "entry1")

	// v1: Create entry
	v1, err := reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.Create, Entry: registry.Entry{
			ID:   entryID,
			Kind: "service",
			Data: payload.NewString("v1"),
		}},
	})
	require.NoError(t, err)

	// v2: Update entry (this should be enriched with OriginalEntry pointing to v1 value)
	v2, err := reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.Update, Entry: registry.Entry{
			ID:   entryID,
			Kind: "service",
			Data: payload.NewString("v2"),
		}},
	})
	require.NoError(t, err)

	// v3: Update entry again (this should be enriched with OriginalEntry pointing to v2 value)
	_, err = reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.Update, Entry: registry.Entry{
			ID:   entryID,
			Kind: "service",
			Data: payload.NewString("v3"),
		}},
	})
	require.NoError(t, err)

	// Close and reopen to force load from database
	err = hist.Close()
	require.NoError(t, err)

	hist2, err := NewSQLite(tmpFile.Name(), logger)
	require.NoError(t, err)
	defer func() { _ = hist2.Close() }()

	// Load changesets from database and verify OriginalEntry is persisted
	cs1, err := hist2.Get(v1)
	require.NoError(t, err)
	require.Len(t, cs1, 1)
	// Create operations should not have OriginalEntry
	assert.Nil(t, cs1[0].OriginalEntry, "Create operation should not have OriginalEntry")

	cs2, err := hist2.Get(v2)
	require.NoError(t, err)
	require.Len(t, cs2, 1)
	// Update operations should have OriginalEntry
	require.NotNil(t, cs2[0].OriginalEntry, "Update operation MUST have OriginalEntry after loading from SQLite")
	assert.Equal(t, "v1", cs2[0].OriginalEntry.Data.Data().(string), "OriginalEntry should contain v1 data")

	// Now create a new registry with the persisted history and try to rollback
	runner2 := &TestRunner{}
	builder2 := topology.NewStateBuilder(logger, nil)
	reg2 := registrysystem.NewRegistry(hist2, runner2, builder2, nil, logger)

	// Load to v3
	head, err := hist2.Head()
	require.NoError(t, err)
	err = reg2.LoadState(ctx, baseline, head)
	require.NoError(t, err)

	// Rollback from v3 to v1 - this will fail if OriginalEntry is missing
	err = reg2.ApplyVersion(ctx, v1)
	require.NoError(t, err, "Rollback should succeed with persisted OriginalEntry")

	// Verify final state
	entries, err := reg2.GetAllEntries()
	require.NoError(t, err)

	var found bool
	for _, e := range entries {
		if e.ID.Equal(entryID) {
			found = true
			assert.Equal(t, "v1", e.Data.Data().(string), "Entry should have v1 value after rollback")
		}
	}
	assert.True(t, found, "Entry should exist after rollback")
}
