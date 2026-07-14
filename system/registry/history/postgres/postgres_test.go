// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	"go.uber.org/zap"
)

func TestNewPostgresRequiresDSN(t *testing.T) {
	hist, err := NewPostgres("", "wippy_registry", zap.NewNop())

	require.Error(t, err)
	assert.Nil(t, hist)
	assert.Contains(t, err.Error(), "history DSN is required")
}

func TestNewPostgresRejectsInvalidSchemaName(t *testing.T) {
	hist, err := NewPostgres("postgres://user:pass@localhost/db?sslmode=disable", "bad-name", zap.NewNop())

	require.Error(t, err)
	assert.Nil(t, hist)
	assert.Contains(t, err.Error(), "invalid schema name")
}

func TestBuildQueriesUseImmutableRowsCASAndCorruptionAwareResolutionRead(t *testing.T) {
	queries := buildQueries("wippy_registry")

	assert.NotContains(t, queries.insertVersion, "ON CONFLICT", "versions must be insert-only")
	assert.NotContains(t, queries.insertChangeset, "ON CONFLICT", "changesets must be insert-only")
	assert.Contains(t, queries.setVersionResolution, "DO NOTHING", "version resolution refs may only be inserted idempotently")
	assert.Contains(t, queries.updateHeadCAS, "value = $2", "head advancement must compare the expected parent")
	assert.Contains(t, queries.setHead, "EXISTS", "SetHead must validate the target version")
	assert.Contains(t, queries.getResolution, "LEFT JOIN", "dangling graph refs must be distinguishable from legacy versions")
	assert.Contains(t, queries.getResolution, "resolution_digest", "the graph row key must be validated against its payload")
	assert.Contains(t, queries.queryMaxVersionID, `"wippy_registry"."versions"`, "max ID lookup must respect the configured schema")
	assert.Contains(t, queries.queryVersionLineage, "UNION", "target lookup must deduplicate cycles")
	assert.NotContains(t, queries.queryVersionLineage, "UNION ALL", "target lookup must terminate on cycles")
}

func TestHistoryRejectsParentlessNonRootVersionBeforeDatabaseAccess(t *testing.T) {
	hist := &History{}
	err := hist.Save(version.New(1), nil, false)
	require.ErrorContains(t, err, "non-root version 1 has no parent")
}

func TestHistoryRejectsMalformedResolutionBeforeDatabaseAccess(t *testing.T) {
	hist := &History{}
	malformed := (&registry.DependencyResolution{Roots: []registry.DependencyRoot{{
		ID: "missing-component", Version: "1",
	}}}).Canonical()
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)

	require.ErrorIs(t, hist.SaveWithDependencyResolution(v1, nil, malformed, false), registry.ErrInvalidDependencyResolution)
	require.ErrorIs(t, hist.CheckpointDependencyResolution(v0, malformed), registry.ErrInvalidDependencyResolution)
	require.ErrorIs(t, hist.CompareAndSetHeadWithDependencyResolution(v0, v0, malformed), registry.ErrInvalidDependencyResolution)
}

func TestPostgresHistory_SaveAndGet(t *testing.T) {
	dsn := os.Getenv("WIPPY_POSTGRES_HISTORY_TEST_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("WIPPY_POSTGRES_HISTORY_TEST_DSN is not set")
	}

	schemaName := fmt.Sprintf("wippy_registry_test_%d", os.Getpid())
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %q CASCADE", schemaName))
	require.NoError(t, err)
	defer func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %q CASCADE", schemaName))
	}()

	hist, err := NewPostgres(dsn, schemaName, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = hist.Close() }()

	v0, err := hist.Head()
	require.NoError(t, err)

	v1 := version.FromParent(v0, 1)
	cs := registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: registry.Entry{ID: registry.NewID("test", "entry1")}},
	}
	require.NoError(t, hist.Save(v1, cs, true))

	head, err := hist.Head()
	require.NoError(t, err)
	assert.Equal(t, uint(1), head.ID())

	retrieved, err := hist.Get(v1)
	require.NoError(t, err)
	require.Len(t, retrieved, 1)
	assert.Equal(t, registry.EntryCreate, retrieved[0].Kind)
	assert.Equal(t, "test", retrieved[0].Entry.ID.NS)
	assert.Equal(t, "entry1", retrieved[0].Entry.ID.Name)

	v2 := version.FromParent(v1, 2)
	require.NoError(t, hist.Save(v2, nil, false))
	resolution := (&registry.DependencyResolution{InputDigest: "postgres-atomic"}).Canonical()
	err = hist.CompareAndSetHeadWithDependencyResolution(v2, v2, resolution)
	require.ErrorContains(t, err, "history head changed")
	_, err = hist.GetDependencyResolution(v2)
	require.ErrorIs(t, err, registry.ErrDependencyResolutionNotFound)
	require.NoError(t, hist.CompareAndSetHeadWithDependencyResolution(v1, v2, resolution))
	storedResolution, err := hist.GetDependencyResolution(v2)
	require.NoError(t, err)
	require.Equal(t, resolution.Digest, storedResolution.Digest)
	var replayed int
	require.NoError(t, hist.ReplayChanges(context.Background(), v2, func(registry.ChangeSet) error {
		replayed++
		return nil
	}))
	require.Equal(t, 2, replayed)

	_, err = db.ExecContext(context.Background(), fmt.Sprintf(`
		INSERT INTO %q.versions (id, parent_id)
		SELECT n, n - 1 FROM generate_series(3, 5000) AS series(n)`, schemaName))
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(), fmt.Sprintf(`
		INSERT INTO %q.changesets (version_id, data)
		SELECT n, $1 FROM generate_series(3, 5000) AS series(n)`, schemaName), []byte{0x90})
	require.NoError(t, err)
	replayed = 0
	require.NoError(t, hist.ReplayChanges(context.Background(), version.New(5000), func(registry.ChangeSet) error {
		replayed++
		return nil
	}))
	require.Equal(t, 5000, replayed)
	stored, err := hist.GetVersion(5000)
	require.NoError(t, err)
	require.Equal(t, uint(5000), stored.ID())
	lineageLength := 0
	for current := stored; current != nil; current = current.Previous() {
		lineageLength++
	}
	require.Equal(t, 5001, lineageLength)

	_, err = db.ExecContext(context.Background(), fmt.Sprintf(`UPDATE %q.versions SET parent_id = 2 WHERE id = 1`, schemaName))
	require.NoError(t, err)
	err = hist.ReplayChanges(context.Background(), v2, func(registry.ChangeSet) error { return nil })
	require.ErrorContains(t, err, "lineage")
	_, err = hist.Head()
	require.Error(t, err, "head reconstruction must terminate and reject cyclic lineage")
	_, err = hist.GetVersion(2)
	require.Error(t, err, "target lookup must terminate and reject cyclic lineage")

	var schemaVersion string
	err = db.QueryRowContext(
		context.Background(),
		fmt.Sprintf(`SELECT curr_version FROM %q.schema_version WHERE name = 'registry_history'`, schemaName),
	).Scan(&schemaVersion)
	require.NoError(t, err)
	assert.Equal(t, "1.1", schemaVersion)
}

func TestPostgresHistory_ConcurrentColdOpenInitializesRootOnce(t *testing.T) {
	dsn := os.Getenv("WIPPY_POSTGRES_HISTORY_TEST_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("WIPPY_POSTGRES_HISTORY_TEST_DSN is not set")
	}

	schemaName := fmt.Sprintf("wippy_registry_concurrent_%d", os.Getpid())
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %q CASCADE", schemaName))
	require.NoError(t, err)
	defer func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %q CASCADE", schemaName))
	}()

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			hist, err := NewPostgres(dsn, schemaName, zap.NewNop())
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

	var versions int
	err = db.QueryRowContext(
		context.Background(),
		fmt.Sprintf(`SELECT COUNT(*) FROM %q.versions WHERE id = 0`, schemaName),
	).Scan(&versions)
	require.NoError(t, err)
	assert.Equal(t, 1, versions)

	var changesets int
	err = db.QueryRowContext(
		context.Background(),
		fmt.Sprintf(`SELECT COUNT(*) FROM %q.changesets WHERE version_id = 0`, schemaName),
	).Scan(&changesets)
	require.NoError(t, err)
	assert.Equal(t, 1, changesets)
}
