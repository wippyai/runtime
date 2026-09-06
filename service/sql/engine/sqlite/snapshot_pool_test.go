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
