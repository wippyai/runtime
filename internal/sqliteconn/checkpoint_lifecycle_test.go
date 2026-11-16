// SPDX-License-Identifier: MPL-2.0

//go:build cgo

package sqliteconn

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

func TestCheckpointAllowsAttachmentRemovalAfterWriterRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	dir := t.TempDir()
	c, err := NewConnector("file:"+filepath.Join(dir, "main.db")+"?_journal_mode=WAL", sqlapi.SQLiteCheckpointConfig{Interval: time.Hour, MaxWALPages: 1024})
	require.NoError(t, err)
	db := sql.OpenDB(c)
	db.SetMaxOpenConns(1)
	c.Attach(db)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	file := filepath.Join(dir, "removed.db")
	_, err = db.ExecContext(ctx, "ATTACH DATABASE ? AS aux", file)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "PRAGMA aux.journal_mode=WAL")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "CREATE TABLE aux.items (value INTEGER)")
	require.NoError(t, err)

	// Hold maintenance until discovery has returned the application connection.
	maintenance, err := c.checkpointer.own.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = maintenance.Close() })
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	waits := c.checkpointer.own.Stats().WaitCount
	done := make(chan error, 1)
	go func() { done <- checkpointAttachments(ctx, c.checkpointer.own, conn) }()
	require.Eventually(t, func() bool {
		return c.checkpointer.own.Stats().WaitCount > waits
	}, 5*time.Second, time.Millisecond)

	_, err = db.ExecContext(ctx, "DETACH DATABASE aux")
	require.NoError(t, err)
	require.NoError(t, os.Remove(file))
	require.NoError(t, maintenance.Close())
	select {
	case err := <-done:
		require.NoError(t, err, "an attachment removed after discovery is no longer maintained")
	case <-ctx.Done():
		t.Fatal("attachment maintenance did not finish")
	}
	_, err = os.Stat(file)
	require.ErrorIs(t, err, os.ErrNotExist, "maintenance must not recreate the detached database")
}
