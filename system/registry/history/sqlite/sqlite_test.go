// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

func TestHistory_ReplayChangesStreamsVeryLongLineage(t *testing.T) {
	hist, err := NewSQLite(filepath.Join(t.TempDir(), "long.db"), zap.NewNop())
	require.NoError(t, err)
	defer func() { require.NoError(t, hist.Close()) }()

	tx, err := hist.db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	ctx := context.Background()
	target := version.New(0)
	const versions = 5000
	for i := 1; i <= versions; i++ {
		_, err = tx.ExecContext(ctx, "INSERT INTO versions (id, parent_id) VALUES (?, ?)", i, i-1)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, "INSERT INTO changesets (version_id, data) VALUES (?, ?)", i, []byte{0x90})
		require.NoError(t, err)
		target = version.FromParent(target, uint(i))
	}
	require.NoError(t, tx.Commit())

	count := 0
	require.NoError(t, hist.ReplayChanges(target, func(cs registry.ChangeSet) error {
		require.Empty(t, cs)
		count++
		return nil
	}))
	require.Equal(t, versions, count)
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

func TestHistory_EnforcesForeignKeysAndRejectsMissingCheckpointVersion(t *testing.T) {
	hist, err := NewSQLite(filepath.Join(t.TempDir(), "history.db"), zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	var enabled int
	require.NoError(t, hist.db.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&enabled))
	require.Equal(t, 1, enabled)

	resolution := (&registry.DependencyResolution{InputDigest: "roots"}).Canonical()
	err = hist.CheckpointDependencyResolution(version.New(99), resolution)
	require.Error(t, err)
	_, err = hist.GetDependencyResolution(version.New(99))
	require.ErrorIs(t, err, registry.ErrDependencyResolutionNotFound)
}

func TestHistory_SaveIsInsertOnlyAndHeadUsesParentCAS(t *testing.T) {
	hist, err := NewSQLite(filepath.Join(t.TempDir(), "history.db"), zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	v0, err := hist.Head()
	require.NoError(t, err)
	v1 := version.FromParent(v0, 1)
	require.NoError(t, hist.Save(v1, nil, true))
	require.Error(t, hist.Save(v1, nil, true), "a history version must never be overwritten")

	v2 := version.FromParent(v1, 2)
	require.NoError(t, hist.Save(v2, nil, true))
	stale := version.FromParent(v1, 3)
	require.ErrorContains(t, hist.Save(stale, nil, true), "history head changed")
	_, err = hist.Get(stale)
	require.Error(t, err, "the failed CAS must roll the version and changeset back")

	require.Error(t, hist.SetHead(version.New(99)))
	head, err := hist.Head()
	require.NoError(t, err)
	require.Equal(t, v2.ID(), head.ID())
}

func TestHistory_ResolutionReferenceIsImmutableAndCorruptionIsNotLegacy(t *testing.T) {
	hist, err := NewSQLite(filepath.Join(t.TempDir(), "history.db"), zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	v0, err := hist.Head()
	require.NoError(t, err)
	v1 := version.FromParent(v0, 1)
	first := (&registry.DependencyResolution{InputDigest: "first"}).Canonical()
	second := (&registry.DependencyResolution{InputDigest: "second"}).Canonical()
	require.NoError(t, hist.SaveWithDependencyResolution(v1, nil, first, true))
	require.NoError(t, hist.CheckpointDependencyResolution(v1, first), "same graph checkpoint is idempotent")
	require.Error(t, hist.CheckpointDependencyResolution(v1, second), "historical graph references are immutable")

	secondData, err := json.Marshal(second)
	require.NoError(t, err)
	_, err = hist.db.ExecContext(context.Background(), "UPDATE resolution_graphs SET data = ? WHERE digest = ?", secondData, first.Digest)
	require.NoError(t, err)
	_, err = hist.GetDependencyResolution(v1)
	require.Error(t, err)
	require.False(t, errors.Is(err, registry.ErrDependencyResolutionNotFound), "key/payload corruption must not trigger legacy resolution")

	_, err = hist.db.ExecContext(context.Background(), "PRAGMA foreign_keys=OFF")
	require.NoError(t, err)
	_, err = hist.db.ExecContext(context.Background(), "UPDATE version_resolutions SET resolution_digest = 'sha256:missing' WHERE version_id = ?", v1.ID())
	require.NoError(t, err)
	_, err = hist.db.ExecContext(context.Background(), "PRAGMA foreign_keys=ON")
	require.NoError(t, err)
	_, err = hist.GetDependencyResolution(v1)
	require.Error(t, err)
	require.False(t, errors.Is(err, registry.ErrDependencyResolutionNotFound), "a dangling graph reference is corruption, not a legacy version")
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
	err = hist.Save(v1, cs1, true)
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

func TestHistory_MigratesV10WithoutChangingExistingHistory(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "history-v1.0.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	statements := []string{
		`CREATE TABLE schema_version (name TEXT PRIMARY KEY, curr_version TEXT NOT NULL, min_compatible_version TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE schema_update_history (name TEXT NOT NULL, update_time TEXT NOT NULL, old_version TEXT NOT NULL, new_version TEXT NOT NULL, manifest_sha256 TEXT NOT NULL, description TEXT, PRIMARY KEY (name, update_time, new_version))`,
		`INSERT INTO schema_version (name, curr_version, min_compatible_version, updated_at) VALUES ('registry_history', '1.0', '1.0', '2025-01-01T00:00:00Z')`,
		`CREATE TABLE metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE versions (id INTEGER PRIMARY KEY, parent_id INTEGER, FOREIGN KEY (parent_id) REFERENCES versions(id))`,
		`CREATE TABLE changesets (version_id INTEGER PRIMARY KEY, data BLOB NOT NULL, FOREIGN KEY (version_id) REFERENCES versions(id) ON DELETE CASCADE)`,
		`CREATE INDEX idx_versions_parent ON versions(parent_id)`,
		`INSERT INTO versions (id, parent_id) VALUES (0, NULL), (1, 0)`,
		`INSERT INTO changesets (version_id, data) VALUES (0, X'90'), (1, X'90')`,
		`INSERT INTO metadata (key, value) VALUES ('head', '1')`,
	}
	for _, statement := range statements {
		_, execErr := db.ExecContext(context.Background(), statement)
		require.NoError(t, execErr)
	}
	require.NoError(t, db.Close())

	hist, err := NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()
	head, err := hist.Head()
	require.NoError(t, err)
	require.Equal(t, uint(1), head.ID())
	changes, err := hist.Get(head)
	require.NoError(t, err)
	require.Empty(t, changes)
	versions, err := hist.Versions()
	require.NoError(t, err)
	require.Len(t, versions, 2)
	_, err = hist.GetDependencyResolution(head)
	require.ErrorIs(t, err, registry.ErrDependencyResolutionNotFound, "pre-1.1 versions remain explicit legacy versions")

	var schemaVersion string
	require.NoError(t, hist.db.QueryRowContext(context.Background(),
		"SELECT curr_version FROM schema_version WHERE name = 'registry_history'").Scan(&schemaVersion))
	require.Equal(t, "1.1", schemaVersion)
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
