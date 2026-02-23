// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"context"
	"os"
	"path/filepath"
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

type testRunner struct{}

func (r *testRunner) Transition(_ context.Context, from registry.State, cs registry.ChangeSet) (registry.State, error) {
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
	return result, nil
}

func TestHistory_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	hist, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	head, err := hist.Head()
	require.NoError(t, err)
	assert.Equal(t, uint(0), head.ID())
}

func TestHistory_SaveAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	hist, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	v0, err := hist.Head()
	require.NoError(t, err)

	v1 := version.FromParent(v0, 1)
	cs := registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: registry.Entry{ID: registry.NewID("test", "entry1")}},
	}

	err = hist.Save(v1, cs, true)
	require.NoError(t, err)

	head, err := hist.Head()
	require.NoError(t, err)
	assert.Equal(t, uint(1), head.ID())

	retrieved, err := hist.Get(v1)
	require.NoError(t, err)
	assert.Len(t, retrieved, 1)
	assert.Equal(t, registry.EntryCreate, retrieved[0].Kind)
	assert.Equal(t, "test", retrieved[0].Entry.ID.NS)
}

func TestHistory_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	hist1, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)

	v0, err := hist1.Head()
	require.NoError(t, err)

	v1 := version.FromParent(v0, 1)
	cs := registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: registry.Entry{ID: registry.NewID("test", "entry1")}},
	}

	err = hist1.Save(v1, cs, true)
	require.NoError(t, err)
	_ = hist1.Close()

	hist2, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist2.Close() }()

	head, err := hist2.Head()
	require.NoError(t, err)
	assert.Equal(t, uint(1), head.ID())

	versions, err := hist2.Versions()
	require.NoError(t, err)
	assert.Len(t, versions, 2)
}

func TestHistory_Versions(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	hist, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	v0, err := hist.Head()
	require.NoError(t, err)

	v1 := version.FromParent(v0, 1)
	cs1 := registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: registry.Entry{ID: registry.NewID("test", "entry1")}},
	}
	err = hist.Save(v1, cs1, true)
	require.NoError(t, err)

	v2 := version.FromParent(v1, 2)
	cs2 := registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: registry.Entry{ID: registry.NewID("test", "entry2")}},
	}
	err = hist.Save(v2, cs2, true)
	require.NoError(t, err)

	versions, err := hist.Versions()
	require.NoError(t, err)
	assert.Len(t, versions, 3)
	assert.Equal(t, uint(0), versions[0].ID())
	assert.Equal(t, uint(1), versions[1].ID())
	assert.Equal(t, uint(2), versions[2].ID())
}

func TestHistory_SetHead(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	hist, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	v0, err := hist.Head()
	require.NoError(t, err)

	v1 := version.FromParent(v0, 1)
	cs1 := registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: registry.Entry{ID: registry.NewID("test", "entry1")}},
	}
	err = hist.Save(v1, cs1, false)
	require.NoError(t, err)

	v2 := version.FromParent(v1, 2)
	cs2 := registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: registry.Entry{ID: registry.NewID("test", "entry2")}},
	}
	err = hist.Save(v2, cs2, true)
	require.NoError(t, err)

	head, err := hist.Head()
	require.NoError(t, err)
	assert.Equal(t, uint(2), head.ID())

	err = hist.SetHead(v1)
	require.NoError(t, err)

	head, err = hist.Head()
	require.NoError(t, err)
	assert.Equal(t, uint(1), head.ID())
}

func TestHistory_NotFoundError(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	hist, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	v999 := version.New(999)
	_, err = hist.Get(v999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changeset not found")
}

func TestHistory_DatabaseFileCreation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	_, err := os.Stat(dbPath)
	assert.True(t, os.IsNotExist(err))

	hist, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	_, err = os.Stat(dbPath)
	assert.NoError(t, err)
}

func TestSQLitePersistence_OriginalEntry(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	tmpFile, err := os.CreateTemp("", "test-*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	hist, err := NewSQLite(tmpFile.Name(), logger)
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	runner := &testRunner{}
	builder := topology.NewStateBuilder(logger, nil)
	reg := registrysystem.NewRegistry(hist, runner, builder, nil, logger)

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

	v1, err := reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: registry.Entry{
			ID:   entryID,
			Kind: "service",
			Data: payload.NewString("v1"),
		}},
	})
	require.NoError(t, err)

	v2, err := reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryUpdate, Entry: registry.Entry{
			ID:   entryID,
			Kind: "service",
			Data: payload.NewString("v2"),
		}},
	})
	require.NoError(t, err)

	_, err = reg.Apply(ctx, registry.ChangeSet{
		{Kind: registry.EntryUpdate, Entry: registry.Entry{
			ID:   entryID,
			Kind: "service",
			Data: payload.NewString("v3"),
		}},
	})
	require.NoError(t, err)

	err = hist.Close()
	require.NoError(t, err)

	hist2, err := NewSQLite(tmpFile.Name(), logger)
	require.NoError(t, err)
	defer func() { _ = hist2.Close() }()

	cs1, err := hist2.Get(v1)
	require.NoError(t, err)
	require.Len(t, cs1, 1)
	assert.Nil(t, cs1[0].OriginalEntry, "Create operation should not have OriginalEntry")

	cs2, err := hist2.Get(v2)
	require.NoError(t, err)
	require.Len(t, cs2, 1)
	require.NotNil(t, cs2[0].OriginalEntry, "Update operation MUST have OriginalEntry after loading from SQLite")
	assert.Equal(t, "v1", cs2[0].OriginalEntry.Data.Data().(string), "OriginalEntry should contain v1 data")

	runner2 := &testRunner{}
	builder2 := topology.NewStateBuilder(logger, nil)
	reg2 := registrysystem.NewRegistry(hist2, runner2, builder2, nil, logger)

	head, err := hist2.Head()
	require.NoError(t, err)
	err = reg2.LoadState(ctx, baseline, head)
	require.NoError(t, err)

	err = reg2.ApplyVersion(ctx, v1)
	require.NoError(t, err, "Rollback should succeed with persisted OriginalEntry")

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
