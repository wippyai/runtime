// SPDX-License-Identifier: MPL-2.0

//go:build cgo

package sqliteconn

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

func defaultCheckpoint() sqlapi.SQLiteCheckpointConfig {
	var cfg sqlapi.SQLiteCheckpointConfig
	cfg.InitDefaults()
	return cfg
}

func TestOpenPreservesSynchronization(t *testing.T) {
	for _, test := range []struct {
		name string
		dsn  string
		want int
	}{
		{name: "default", want: 1},
		{name: "full", dsn: "&_synchronous=FULL", want: 2},
		{name: "off", dsn: "&_synchronous=OFF", want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, err := Open("file:"+filepath.Join(t.TempDir(), "app.db")+"?_journal_mode=WAL"+test.dsn, defaultCheckpoint())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			var synchronous, checkpoint int
			require.NoError(t, db.QueryRowContext(context.Background(), "PRAGMA synchronous").Scan(&synchronous))
			require.Equal(t, test.want, synchronous)
			require.NoError(t, db.QueryRowContext(context.Background(), "PRAGMA wal_autocheckpoint").Scan(&checkpoint))
			require.Equal(t, 0, checkpoint)
		})
	}
}

func TestOpenReadOnly(t *testing.T) {
	file := filepath.Join(t.TempDir(), "app.db")
	ctx := context.Background()
	writer, err := Open("file:"+file+"?_journal_mode=WAL", defaultCheckpoint())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, writer.Close()) })
	_, err = writer.ExecContext(ctx, "CREATE TABLE items (id INTEGER)")
	require.NoError(t, err)
	reader, err := Open("file:"+file+"?mode=ro", defaultCheckpoint())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	reader.SetMaxOpenConns(1)
	// Force a replacement as well as the initial read-only connection.
	for i := 0; i < 2; i++ {
		var checkpoint int
		require.NoError(t, reader.QueryRowContext(ctx, "PRAGMA wal_autocheckpoint").Scan(&checkpoint))
		require.Equal(t, 0, checkpoint)
		_, err = reader.ExecContext(ctx, "INSERT INTO items VALUES (1)")
		require.ErrorContains(t, err, "readonly")
		reader.SetMaxIdleConns(0)
		reader.SetMaxIdleConns(1)
	}
}

func TestConnectorOwnsCheckpointerOnlyForWritableFiles(t *testing.T) {
	file := filepath.Join(t.TempDir(), "app.db")
	for _, test := range []struct {
		dsn  string
		want bool
	}{
		{dsn: "file:" + file + "?mode=rwc", want: true},
		{dsn: file, want: true},
		{dsn: "file:" + file + "?mode=ro"},
		{dsn: "file:" + file + "?immutable=1"},
		{dsn: ":memory:"},
		{dsn: "file::memory:?mode=memory&_foreign_keys=1"},
		{dsn: "file:shared?mode=memory&cache=shared"},
	} {
		t.Run(test.dsn, func(t *testing.T) {
			c, err := NewConnector(test.dsn, defaultCheckpoint())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, c.Close()) })
			require.Equal(t, test.want, c.checkpointer != nil)
		})
	}
}

func TestConnectorRejectsInvalidCheckpointPolicy(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "app.db")
	_, err := NewConnector(dsn, sqlapi.SQLiteCheckpointConfig{MaxWALPages: 1})
	require.ErrorContains(t, err, "interval")
	_, err = NewConnector(dsn, sqlapi.SQLiteCheckpointConfig{Interval: time.Second})
	require.ErrorContains(t, err, "max WAL pages")
}

func TestCloseUnusedPoolKeepsOpenLazy(t *testing.T) {
	file := filepath.Join(t.TempDir(), "unused.db")
	db, err := Open("file:"+file+"?_journal_mode=WAL", defaultCheckpoint())
	require.NoError(t, err)
	require.NoError(t, db.Close())
	_, err = os.Stat(file)
	require.True(t, os.IsNotExist(err), "closing an unused pool must not create the database")
}
