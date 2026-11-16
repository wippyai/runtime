// SPDX-License-Identifier: MPL-2.0

//go:build cgo

package sqliteconn

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/rqlite/go-sqlite3"
	"github.com/stretchr/testify/require"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

func TestAttachedWALRetainsCheckpointCoverage(t *testing.T) {
	for _, mode := range []string{"managed", "memory", "readonly"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			dsn := "file:" + filepath.Join(dir, "main.db") + "?_journal_mode=WAL"
			if mode == "memory" {
				dsn = ":memory:"
			}
			if mode == "readonly" {
				seed, err := Open(dsn, defaultCheckpoint())
				require.NoError(t, err)
				_, err = seed.ExecContext(context.Background(), "CREATE TABLE seed (id INTEGER)")
				require.NoError(t, err)
				require.NoError(t, seed.Close())
				dsn += "&mode=ro"
			}
			cfg := defaultCheckpoint()
			cfg.Interval = 10 * time.Millisecond
			db, err := Open(dsn, cfg)
			require.NoError(t, err)
			db.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			file := filepath.Join(dir, "attached.db")
			ctx := context.Background()
			_, err = db.ExecContext(ctx, `ATTACH DATABASE ? AS "extra""db"`, file)
			require.NoError(t, err)
			_, err = db.ExecContext(ctx, `PRAGMA "extra""db".journal_mode=WAL`)
			require.NoError(t, err)
			_, err = db.ExecContext(ctx, `CREATE TABLE "extra""db".items (value BLOB)`)
			require.NoError(t, err)
			tx, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = tx.Rollback() })
			for range 2000 {
				_, err = tx.ExecContext(ctx, `INSERT INTO "extra""db".items VALUES (zeroblob(3000))`)
				require.NoError(t, err)
			}
			require.NoError(t, tx.Commit())
			require.Eventually(t, func() bool {
				return sizeOf(t, file) >= 2000*testPageSize
			}, 5*time.Second, 10*time.Millisecond, "attached WAL must backfill while the pool stays open")
		})
	}
}

func TestTransientCheckpointFailureDoesNotPoisonClose(t *testing.T) {
	for _, code := range []sqlite3.ErrNo{sqlite3.ErrBusy, sqlite3.ErrLocked} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			c, err := NewConnector("file:"+filepath.Join(t.TempDir(), "app.db"), defaultCheckpoint())
			require.NoError(t, err)
			c.checkpointer.record(fmt.Errorf("checkpoint: %w", sqlite3.Error{Code: code, ExtendedCode: code.Extend(2)}))
			require.NoError(t, c.Close(), "transient maintenance contention must not become a shutdown failure")
		})
	}
}

func TestCheckpointFaultIsReportedOnClose(t *testing.T) {
	c, err := NewConnector("file:"+filepath.Join(t.TempDir(), "app.db"), defaultCheckpoint())
	require.NoError(t, err)
	fault := errors.New("storage I/O failed")
	c.checkpointer.record(fault)
	require.ErrorIs(t, c.Close(), fault)
}

func TestReadOnlyAttachmentDoesNotFailMaintenance(t *testing.T) {
	file := filepath.Join(t.TempDir(), "readonly.db")
	seed, err := Open("file:"+file+"?_journal_mode=WAL", defaultCheckpoint())
	require.NoError(t, err)
	_, err = seed.ExecContext(context.Background(), "CREATE TABLE items (id INTEGER)")
	require.NoError(t, err)
	require.NoError(t, seed.Close())
	cfg := sqlapi.SQLiteCheckpointConfig{Interval: time.Hour, MaxWALPages: 1024}
	c, err := NewConnector("file:"+filepath.Join(t.TempDir(), "main.db")+"?_journal_mode=WAL", cfg)
	require.NoError(t, err)
	db := sql.OpenDB(c)
	db.SetMaxOpenConns(1)
	c.Attach(db)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.ExecContext(context.Background(), "ATTACH DATABASE ? AS readonly_aux", "file:"+file+"?mode=ro")
	require.NoError(t, err)
	require.NoError(t, c.checkpointer.checkpoint(context.Background()))
}
