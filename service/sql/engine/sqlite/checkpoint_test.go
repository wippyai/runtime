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
		require.Equal(t, 1000, checkpoint)
		require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous))
		require.Equal(t, 1, synchronous, "preserve NORMAL synchronization")
		// A pool-level initialization would be lost with this connection.
		_, err = conn.ExecContext(ctx, "PRAGMA wal_autocheckpoint=0")
		require.NoError(t, err)
		require.ErrorIs(t, conn.Raw(func(any) error { return driver.ErrBadConn }), driver.ErrBadConn)
		_ = conn.Close()
	}
}

func TestSQLiteWALCheckpointProgress(t *testing.T) {
	opened, file := openCheckpointDB(t)
	ctx := context.Background()
	_, err := opened.DB.ExecContext(ctx, "CREATE TABLE items (id INTEGER PRIMARY KEY, value BLOB)")
	require.NoError(t, err)
	for i := 0; i < 1500; i++ {
		_, err = opened.DB.ExecContext(ctx, "INSERT INTO items (value) VALUES (zeroblob(4096))")
		require.NoError(t, err)
	}
	// This controlled workload has no pinned readers or large transactions.
	// Check before close (which checkpoints too), not after a manual checkpoint.
	info, err := os.Stat(file + "-wal")
	require.NoError(t, err)
	require.Less(t, info.Size(), int64(8<<20), "automatic checkpoints must recycle this WAL while the pool stays open")
}
