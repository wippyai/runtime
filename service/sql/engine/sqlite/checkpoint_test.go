// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"context"
	"database/sql/driver"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	config "github.com/wippyai/runtime/api/service/sql"
	sqlservice "github.com/wippyai/runtime/service/sql"
)

func openCheckpointDB(t *testing.T) (sqlservice.OpenedDB, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "checkpoint.db")
	cfg := &config.SQLiteConfig{File: file, Pool: config.PoolConfig{MaxLifetime: time.Hour}}
	opened, err := (engine{}).Open(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if opened.Observer != nil {
			require.NoError(t, opened.Observer.Close())
		}
		require.NoError(t, opened.DB.Close())
	})
	(engine{}).Tune(opened.DB, cfg)
	require.NoError(t, (engine{}).Prepare(context.Background(), opened.DB, cfg))
	return opened, file
}

func TestSQLiteWALPolicySurvivesConnectionReplacement(t *testing.T) {
	opened, _ := openCheckpointDB(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		conn, err := opened.DB.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var mode string
		var checkpoint, synchronous int
		require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode))
		require.Equal(t, "wal", mode)
		require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA wal_autocheckpoint").Scan(&checkpoint))
		require.Equal(t, 0, checkpoint, "writer connections never checkpoint inline")
		require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous))
		require.Equal(t, 1, synchronous, "preserve NORMAL synchronization")
		// A pool-level initialization would be lost with this connection.
		_, err = conn.ExecContext(ctx, "PRAGMA wal_autocheckpoint=1000")
		require.NoError(t, err)
		require.ErrorIs(t, conn.Raw(func(any) error { return driver.ErrBadConn }), driver.ErrBadConn)
		_ = conn.Close()
	}
}

// writeLargeTransaction commits rows that dirty well over SQLite's default
// 1000-page autocheckpoint threshold in a single transaction.
func writeLargeTransaction(t *testing.T, opened sqlservice.OpenedDB, rows int) {
	t.Helper()
	ctx := context.Background()
	_, err := opened.DB.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS items (id INTEGER PRIMARY KEY, value BLOB)")
	require.NoError(t, err)
	tx, err := opened.DB.BeginTx(ctx, nil)
	require.NoError(t, err)
	for i := 0; i < rows; i++ {
		_, err = tx.ExecContext(ctx, "INSERT INTO items (value) VALUES (zeroblob(4096))")
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	return info.Size()
}

func TestSQLiteCommitLeavesCheckpointToBackgroundCheckpointer(t *testing.T) {
	opened, file := openCheckpointDB(t)
	writeLargeTransaction(t, opened, 1000)

	// The committed pages live only in the WAL: the commit path copied none of
	// them into the database file.
	require.Less(t, fileSize(t, file), int64(1<<20), "commit must not checkpoint inline")
	require.Greater(t, fileSize(t, file+"-wal"), int64(4<<20))

	// The background checkpointer backfills the database file while the pool
	// stays open.
	require.Eventually(t, func() bool {
		return fileSize(t, file) > 4<<20
	}, 10*time.Second, 10*time.Millisecond, "background checkpointer must drain the WAL")
}
