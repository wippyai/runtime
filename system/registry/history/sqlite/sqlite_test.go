// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"sync"
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

func TestHistory_CreatesParentDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "nested", "registry", "history.db")

	hist, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	_, err = os.Stat(dbPath)
	require.NoError(t, err)
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

func TestHistory_DependencyResolutionIsAtomicDeduplicatedAndInherited(t *testing.T) {
	hist, err := NewSQLite(filepath.Join(t.TempDir(), "history.db"), zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	v0, err := hist.Head()
	require.NoError(t, err)
	v1 := version.FromParent(v0, 1)
	resolution := (&registry.DependencyResolution{
		InputDigest: "roots",
		Modules: []registry.ResolvedModule{{
			Name: "acme/crm", Version: "v1.6.0", VersionID: "crm-16", Digest: "sha256:crm",
		}},
	}).Canonical()
	require.NoError(t, hist.SaveWithDependencyResolution(v1, registry.ChangeSet{{
		Kind: registry.EntryCreate, Entry: registry.Entry{ID: registry.NewID("app.deps", "crm")},
	}}, resolution, true))

	got, err := hist.GetDependencyResolution(v1)
	require.NoError(t, err)
	require.Equal(t, resolution, got)

	v2 := version.FromParent(v1, 2)
	require.NoError(t, hist.Save(v2, registry.ChangeSet{{
		Kind: registry.EntryCreate, Entry: registry.Entry{ID: registry.NewID("app", "unrelated")},
	}}, true))
	inherited, err := hist.GetDependencyResolution(v2)
	require.NoError(t, err)
	require.Equal(t, resolution.Digest, inherited.Digest)

	var graphs int
	require.NoError(t, hist.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM resolution_graphs").Scan(&graphs))
	require.Equal(t, 1, graphs, "versions sharing a graph must store its payload once")
	var refs int
	require.NoError(t, hist.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM version_resolutions").Scan(&refs))
	require.Equal(t, 2, refs)
}

func TestHistory_DependencyResolutionFailureRollsBackVersionAndHead(t *testing.T) {
	hist, err := NewSQLite(filepath.Join(t.TempDir(), "history.db"), zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	_, err = hist.db.ExecContext(context.Background(), `CREATE TRIGGER reject_resolution
		BEFORE INSERT ON resolution_graphs
		BEGIN SELECT RAISE(FAIL, 'injected resolution failure'); END`)
	require.NoError(t, err)
	v0, err := hist.Head()
	require.NoError(t, err)
	v1 := version.FromParent(v0, 1)
	err = hist.SaveWithDependencyResolution(v1, registry.ChangeSet{{
		Kind: registry.EntryCreate, Entry: registry.Entry{ID: registry.NewID("app.deps", "crm")},
	}}, (&registry.DependencyResolution{
		InputDigest: "roots",
		Modules:     []registry.ResolvedModule{{Name: "acme/crm", Version: "v1.6.0"}},
	}).Canonical(), true)
	require.ErrorContains(t, err, "injected resolution failure")

	head, err := hist.Head()
	require.NoError(t, err)
	require.Equal(t, uint(0), head.ID())
	versions, err := hist.Versions()
	require.NoError(t, err)
	require.Len(t, versions, 1)
	_, err = hist.Get(v1)
	require.Error(t, err)
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

func TestHistoryReplayUpdateReplacesCanonicalBaselineID(t *testing.T) {
	hist, err := NewSQLite(filepath.Join(t.TempDir(), "history.db"), zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()
	v0, err := hist.Head()
	require.NoError(t, err)
	id := registry.NewID("app.deps", "agent")
	baseline := registry.Entry{ID: id, Kind: registry.NamespaceDependency, Data: payload.New(map[string]any{"version": "*"})}
	updated := baseline
	updated.Data = payload.New(map[string]any{"version": "0.4.14"})
	v1 := version.FromParent(v0, 1)
	require.NoError(t, hist.Save(v1, registry.ChangeSet{{Kind: registry.EntryUpdate, Entry: updated, OriginalEntry: &baseline}}, true))

	reg := registrysystem.NewRegistry(hist, &testRunner{}, topology.NewStateBuilder(zap.NewNop(), nil), nil, zap.NewNop())
	require.NoError(t, reg.LoadState(context.Background(), registry.State{baseline}, v1))
	entry, err := reg.GetEntry(id)
	require.NoError(t, err)
	data, ok := entry.Data.Data().(map[string]any)
	require.True(t, ok)
	require.Equal(t, "0.4.14", data["version"])
	entries, err := reg.GetAllEntries()
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestHistory_ConcurrentColdOpenInitializesRootOnce(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			hist, err := NewSQLite(dbPath, zap.NewNop())
			if err != nil {
				errs <- err
				return
			}
			errs <- hist.Close()
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	hist, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	versions, err := hist.Versions()
	require.NoError(t, err)
	require.Len(t, versions, 1)
	assert.Equal(t, uint(0), versions[0].ID())

	var changesets int
	err = hist.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM changesets WHERE version_id = 0").Scan(&changesets)
	require.NoError(t, err)
	assert.Equal(t, 1, changesets)
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

func TestHistory_UsesManagedSchemaVersionTable(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	hist, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	var version string
	err = hist.db.QueryRowContext(
		context.Background(),
		"SELECT curr_version FROM schema_version WHERE name = 'registry_history'",
	).Scan(&version)
	require.NoError(t, err)
	assert.Equal(t, "1.1", version)
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
