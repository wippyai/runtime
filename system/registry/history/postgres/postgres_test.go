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

	var schemaVersion string
	err = db.QueryRowContext(
		context.Background(),
		fmt.Sprintf(`SELECT curr_version FROM %q.schema_version WHERE name = 'registry_history'`, schemaName),
	).Scan(&schemaVersion)
	require.NoError(t, err)
	assert.Equal(t, "1.0", schemaVersion)
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
