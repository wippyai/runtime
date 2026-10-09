// SPDX-License-Identifier: MPL-2.0
//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

func TestSnapshotPoolIsReadOnlyAndLazy(t *testing.T) {
	o, err := openObservedDB(t, filepath.Join(t.TempDir(), "app.db"))
	require.NoError(t, err)
	defer o.Close()
	b := o.opened.Observer.(*sqliteBackend)
	require.NotNil(t, b.snapshotDB)
	require.Zero(t, b.snapshotDB.Stats().OpenConnections)
	ctx := context.Background()
	_, err = o.opened.DB.ExecContext(ctx, "CREATE TABLE items(id INTEGER)")
	require.NoError(t, err)
	require.Zero(t, b.snapshotDB.Stats().OpenConnections, "ordinary SQL must not open snapshot connections")
	_, err = b.snapshotDB.ExecContext(ctx, "INSERT INTO items VALUES(1)")
	require.ErrorContains(t, err, "readonly")
	require.NoError(t, b.Close())
	require.Error(t, b.snapshotDB.PingContext(ctx))
}

func TestSnapshotConnectionWaitDoesNotBlockWriterAndCancels(t *testing.T) {
	o, err := openObservedDB(t, filepath.Join(t.TempDir(), "app.db"))
	require.NoError(t, err)
	defer o.Close()
	b := o.opened.Observer.(*sqliteBackend)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = o.opened.DB.ExecContext(ctx, "CREATE TABLE items(id INTEGER)")
	require.NoError(t, err)
	held, err := b.snapshotDB.Conn(ctx)
	require.NoError(t, err)
	defer held.Close()
	waitCtx, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() {
		s, err := b.Snapshot(waitCtx, sqlapi.SnapshotOptions{})
		if s != nil {
			_ = s.Close()
		}
		done <- err
	}()
	require.Eventually(t, func() bool { return b.snapshotDB.Stats().WaitCount > 0 }, time.Second, time.Millisecond)
	_, err = o.opened.DB.ExecContext(ctx, "INSERT INTO items VALUES(1)")
	require.NoError(t, err, "snapshot admission must not hold the commit fence")
	stop()
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestSnapshotPinnedReaderDefersAutomaticCheckpoint(t *testing.T) {
	o, err := openObservedDB(t, filepath.Join(t.TempDir(), "checkpoint.db"))
	require.NoError(t, err)
	defer o.Close()
	b := o.opened.Observer.(*sqliteBackend)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = o.opened.DB.ExecContext(ctx, "CREATE TABLE items (id INTEGER PRIMARY KEY, value BLOB)")
	require.NoError(t, err)
	reader, err := b.snapshotDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer reader.Rollback()
	var count, checkpoint int
	require.NoError(t, reader.QueryRowContext(ctx, "PRAGMA wal_autocheckpoint").Scan(&checkpoint))
	require.Equal(t, 1000, checkpoint, "read-only snapshot connections share initialization")
	require.NoError(t, reader.QueryRowContext(ctx, "SELECT COUNT(*) FROM items").Scan(&count))
	require.Zero(t, count)
	for i := 0; i < 1200; i++ {
		_, err = o.opened.DB.ExecContext(ctx, "INSERT INTO items (value) VALUES (zeroblob(4096))")
		require.NoError(t, err, "checkpoints must not wait for the snapshot reader")
	}
	require.NoError(t, reader.QueryRowContext(ctx, "SELECT COUNT(*) FROM items").Scan(&count))
	require.Zero(t, count, "the pinned snapshot must remain unchanged")
	var busy, logged, checkpointed int
	require.NoError(t, o.opened.DB.QueryRowContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)").Scan(&busy, &logged, &checkpointed))
	require.Zero(t, busy)
	require.Greater(t, logged, 1000)
	require.Less(t, checkpointed, logged, "PASSIVE can be incomplete even when busy is zero")
	require.NoError(t, reader.Rollback())
	// The first commit can now finish the automatic checkpoint; the next one
	// can recycle its frames. No explicit checkpoint runs between these writes.
	for i := 0; i < 2; i++ {
		_, err = o.opened.DB.ExecContext(ctx, "INSERT INTO items (value) VALUES (zeroblob(4096))")
		require.NoError(t, err)
	}
	require.NoError(t, o.opened.DB.QueryRowContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)").Scan(&busy, &logged, &checkpointed))
	require.Zero(t, busy)
	require.Less(t, logged, 1000, "automatic checkpointing must resume after the snapshot releases its WAL frames")
	require.Equal(t, logged, checkpointed)
	require.NoError(t, o.opened.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM items").Scan(&count))
	require.Equal(t, 1202, count)
}
