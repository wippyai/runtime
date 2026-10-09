// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"context"
	"database/sql/driver"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	"go.uber.org/zap"
)

func TestHistoryWALPolicySurvivesConnectionReplacement(t *testing.T) {
	hist, err := NewSQLite(filepath.Join(t.TempDir(), "history.db"), zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, hist.Close()) })
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		conn, err := hist.db.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		var mode string
		var checkpoint, synchronous, foreignKeys, busyTimeout int
		require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode))
		require.Equal(t, "wal", mode)
		require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA wal_autocheckpoint").Scan(&checkpoint))
		require.Equal(t, 1000, checkpoint)
		require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous))
		require.Equal(t, 1, synchronous, "preserve NORMAL synchronization")
		require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys))
		require.Equal(t, 1, foreignKeys)
		require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout))
		require.Equal(t, 5000, busyTimeout)
		_, err = conn.ExecContext(ctx, "PRAGMA wal_autocheckpoint=0")
		require.NoError(t, err)
		require.ErrorIs(t, conn.Raw(func(any) error { return driver.ErrBadConn }), driver.ErrBadConn)
		_ = conn.Close()
	}
}

func TestHistoryWALCheckpointProgress(t *testing.T) {
	file := filepath.Join(t.TempDir(), "history.db")
	hist, err := NewSQLite(file, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, hist.Close()) })
	head, err := hist.Head()
	require.NoError(t, err)
	cs := registry.ChangeSet{{Kind: registry.EntryCreate, Entry: registry.Entry{
		ID: registry.NewID("test", "entry"), Data: payload.NewString(strings.Repeat("x", 4096)),
	}}}
	for i := 1; i <= 1200; i++ {
		head = version.FromParent(head, uint(i))
		require.NoError(t, hist.Save(head, cs, true))
	}
	info, err := os.Stat(file + "-wal")
	require.NoError(t, err)
	require.Less(t, info.Size(), int64(8<<20), "history must recycle the WAL before shutdown")
	got, err := hist.Get(head)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, cs[0].Kind, got[0].Kind)
	require.Equal(t, cs[0].Entry.ID.String(), got[0].Entry.ID.String())
	require.Equal(t, cs[0].Entry.Data, got[0].Entry.Data)
}
