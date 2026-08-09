// SPDX-License-Identifier: MPL-2.0

//go:build integration && sqlite_preupdate_hook

package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/registry"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
	sqlconfig "github.com/wippyai/runtime/api/service/sql"
	sqliteengine "github.com/wippyai/runtime/service/sql/engine/sqlite"
)

type integrationDB struct {
	db        *sql.DB
	observer  sqlapi.CommittedMutationSource
	resources *testResourceRegistry
	source    *Source
}

func openIntegrationDB(t *testing.T, opts sourceOptions) *integrationDB {
	t.Helper()
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "cdc.db")
	cfg := &sqlconfig.SQLiteConfig{File: file}
	cfg.InitDefaults()
	driver := sqliteengine.NewDriver()
	opened, err := driver.Open(ctx, cfg)
	require.NoError(t, err)
	require.NoError(t, driver.Prepare(ctx, opened.DB, cfg))
	driver.Tune(opened.DB, cfg)
	require.NotNil(t, opened.Observer)

	resources := &testResourceRegistry{observer: opened.Observer}
	if opts.res == nil {
		opts.res = resources
	}
	if opts.id.Name == "" {
		opts.id = registry.NewID("app", "sqlite-cdc")
	}
	if opts.name == "" {
		opts.name = opts.id.String()
	}
	sourceValue, err := buildSource(opts)
	require.NoError(t, err)
	source := sourceValue.(*Source)

	result := &integrationDB{db: opened.DB, observer: opened.Observer, resources: resources, source: source}
	t.Cleanup(func() {
		_ = source.Stop(context.Background())
		_ = opened.Observer.Close()
		_ = opened.DB.Close()
	})
	return result
}

func requireNoChange(t *testing.T, stream cdcapi.Stream) {
	t.Helper()
	select {
	case change, ok := <-stream.Changes():
		if !ok {
			if errStream, isErrStream := stream.(cdcapi.ErrStream); isErrStream {
				t.Fatalf("stream closed unexpectedly: %v", errStream.Err())
			}
			t.Fatal("stream closed unexpectedly")
		}
		t.Fatalf("unexpected CDC change: %#v", change)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestIntegrationLiveCommitAndFilters(t *testing.T) {
	db := openIntegrationDB(t, sourceOptions{tables: []string{"users"}})
	_, err := db.db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	_, err = db.db.Exec(`CREATE TABLE audit (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	require.NoError(t, startSourceForIntegration(db.source))

	stream, err := db.source.Subscribe(context.Background(), cdcapi.StreamOptions{Ops: []string{"insert"}})
	require.NoError(t, err)
	defer stream.Close()

	_, err = db.db.Exec(`INSERT INTO users (id, value) VALUES (1, 'one')`)
	require.NoError(t, err)
	change := receiveChange(t, stream)
	assert.Equal(t, "insert", change.Op)
	assert.Equal(t, "users", change.Table)
	assert.Equal(t, int64(1), change.After["id"])

	_, err = db.db.Exec(`UPDATE users SET value = 'two' WHERE id = 1`)
	require.NoError(t, err)
	requireNoChange(t, stream)
	_, err = db.db.Exec(`INSERT INTO audit (id, value) VALUES (1, 'ignored')`)
	require.NoError(t, err)
	requireNoChange(t, stream)
}

func startSourceForIntegration(source *Source) error {
	_, err := source.Start(context.Background())
	return err
}

func TestIntegrationRollbackAndSavepointPublishOnlyCommittedRows(t *testing.T) {
	db := openIntegrationDB(t, sourceOptions{})
	_, err := db.db.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	require.NoError(t, startSourceForIntegration(db.source))
	stream, err := db.source.Subscribe(context.Background(), cdcapi.StreamOptions{})
	require.NoError(t, err)
	defer stream.Close()

	tx, err := db.db.Begin()
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT INTO items (id, value) VALUES (1, 'kept')`)
	require.NoError(t, err)
	_, err = tx.Exec(`SAVEPOINT nested`)
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT INTO items (id, value) VALUES (2, 'discarded')`)
	require.NoError(t, err)
	_, err = tx.Exec(`ROLLBACK TO SAVEPOINT nested`)
	require.NoError(t, err)
	_, err = tx.Exec(`RELEASE SAVEPOINT nested`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	change := receiveChange(t, stream)
	assert.Equal(t, int64(1), change.After["id"])
	assert.Equal(t, []byte("kept"), change.After["value"])
	requireNoChange(t, stream)

	_, err = db.db.Exec(`INSERT INTO items (id, value) VALUES (3, 'rolled back')`)
	require.NoError(t, err)
	change = receiveChange(t, stream)
	assert.Equal(t, int64(3), change.After["id"])
}

func TestIntegrationFailedStatementFailsClosed(t *testing.T) {
	db := openIntegrationDB(t, sourceOptions{})
	_, err := db.db.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT UNIQUE)`)
	require.NoError(t, err)
	require.NoError(t, startSourceForIntegration(db.source))
	stream, err := db.source.Subscribe(context.Background(), cdcapi.StreamOptions{})
	require.NoError(t, err)
	defer stream.Close()

	tx, err := db.db.Begin()
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT OR FAIL INTO items (id, value) VALUES (1, 'same'), (2, 'same')`)
	assert.Error(t, err)
	require.NoError(t, tx.Commit())

	var count int
	require.NoError(t, db.db.QueryRow(`SELECT count(*) FROM items`).Scan(&count))
	assert.Equal(t, 1, count, "SQLite must retain the applied prefix")
	streamErr := waitStreamClosed(t, stream)
	require.Error(t, streamErr)
	assert.Contains(t, streamErr.Error(), "cannot determine statement outcome")
}

func TestIntegrationSubscriberOverflowDoesNotRollbackApplication(t *testing.T) {
	db := openIntegrationDB(t, sourceOptions{})
	_, err := db.db.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	require.NoError(t, startSourceForIntegration(db.source))
	stream, err := db.source.Subscribe(context.Background(), cdcapi.StreamOptions{Buffer: 1})
	require.NoError(t, err)

	for i := 1; i <= 32; i++ {
		_, err = db.db.Exec(`INSERT INTO items (id, value) VALUES (?, 'value')`, i)
		require.NoError(t, err)
	}
	err = waitStreamClosed(t, stream)
	assert.ErrorIs(t, err, errSubscriberOverflow)
	var count int
	require.NoError(t, db.db.QueryRow(`SELECT count(*) FROM items`).Scan(&count))
	assert.Equal(t, 32, count)
}

func TestIntegrationSQLGenerationCloseFaultsSource(t *testing.T) {
	db := openIntegrationDB(t, sourceOptions{})
	_, err := db.db.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	require.NoError(t, startSourceForIntegration(db.source))
	stream, err := db.source.Subscribe(context.Background(), cdcapi.StreamOptions{})
	require.NoError(t, err)

	require.NoError(t, db.observer.Close())
	assert.Error(t, waitStreamClosed(t, stream))
	assert.Equal(t, cdcapi.SourceStateFaulted, db.source.Info().State)
}

func TestIntegrationSnapshotHandoffIsPerSubscriber(t *testing.T) {
	db := openIntegrationDB(t, sourceOptions{})
	_, err := db.db.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	_, err = db.db.Exec(`INSERT INTO items (id, value) VALUES (1, 'existing')`)
	require.NoError(t, err)
	require.NoError(t, startSourceForIntegration(db.source))
	stream, err := db.source.Subscribe(context.Background(), cdcapi.StreamOptions{Snapshot: true})
	require.NoError(t, err)
	defer stream.Close()

	snapshot := receiveChange(t, stream)
	assert.Equal(t, "snapshot", snapshot.Op)
	assert.Equal(t, int64(1), snapshot.After["id"])
	assert.NotEmpty(t, snapshot.Cursor)

	_, err = db.db.Exec(`INSERT INTO items (id, value) VALUES (2, 'live')`)
	require.NoError(t, err)
	live := receiveChange(t, stream)
	assert.Equal(t, "insert", live.Op)
	assert.Equal(t, int64(2), live.After["id"])
}
