// SPDX-License-Identifier: MPL-2.0

//go:build cgo

package sqliteconn

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

const testPageSize = 4096

func openCheckpointed(t *testing.T, cfg sqlapi.SQLiteCheckpointConfig) (*sql.DB, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "app.db")
	db, err := Open("file:"+file+"?_journal_mode=WAL&_busy_timeout=5000", cfg)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.ExecContext(context.Background(), "CREATE TABLE items (id INTEGER PRIMARY KEY, value BLOB)")
	require.NoError(t, err)
	return db, file
}

// insertRows commits rows of one page each in a single transaction.
func insertRows(t *testing.T, db *sql.DB, rows int) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	for i := 0; i < rows; i++ {
		_, err = tx.ExecContext(context.Background(), "INSERT INTO items (value) VALUES (zeroblob(3000))")
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())
}

func sizeOf(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	return info.Size()
}

// pinReader opens a read transaction that holds its WAL snapshot until the
// returned function runs.
func pinReader(t *testing.T, file string) func() {
	t.Helper()
	reader, err := Open("file:"+file+"?mode=ro", defaultCheckpoint())
	require.NoError(t, err)
	tx, err := reader.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	var count int
	require.NoError(t, tx.QueryRowContext(context.Background(), "SELECT count(*) FROM items").Scan(&count))
	return func() {
		require.NoError(t, tx.Rollback())
		require.NoError(t, reader.Close())
	}
}

func TestCommitLeavesCheckpointToCheckpointer(t *testing.T) {
	db, file := openCheckpointed(t, sqlapi.SQLiteCheckpointConfig{Interval: time.Hour, MaxWALPages: 1 << 20})

	// One transaction well over SQLite's default 1000-page autocheckpoint.
	insertRows(t, db, 2000)
	require.Less(t, sizeOf(t, file), int64(64*testPageSize), "commit must not checkpoint inline")
	require.Greater(t, sizeOf(t, file+"-wal"), int64(2000*testPageSize))

	// Close runs the final checkpoint.
	require.NoError(t, db.Close())
	require.Greater(t, sizeOf(t, file), int64(2000*testPageSize))
	require.Zero(t, sizeOf(t, file+"-wal"))
}

func TestCheckpointerDrainsWALWhileOpen(t *testing.T) {
	db, file := openCheckpointed(t, sqlapi.SQLiteCheckpointConfig{Interval: 10 * time.Millisecond, MaxWALPages: 1 << 20})
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	insertRows(t, db, 2000)
	require.Eventually(t, func() bool {
		return sizeOf(t, file) > 2000*testPageSize
	}, 10*time.Second, 10*time.Millisecond)
}

func TestCheckpointerTruncatesWALOverBoundAfterReaderReleases(t *testing.T) {
	db, file := openCheckpointed(t, sqlapi.SQLiteCheckpointConfig{Interval: 10 * time.Millisecond, MaxWALPages: 256})
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	release := pinReader(t, file)
	insertRows(t, db, 1000)
	walPinned := sizeOf(t, file+"-wal")
	require.Greater(t, walPinned, int64(1000*testPageSize))
	release()

	// PASSIVE never shrinks the WAL file; only the TRUNCATE escalation does.
	require.Eventually(t, func() bool {
		return sizeOf(t, file+"-wal") == 0
	}, 10*time.Second, 10*time.Millisecond, "WAL over the bound must be truncated")
	require.Greater(t, sizeOf(t, file), int64(1000*testPageSize))
}

func TestCheckpointerBoundsWALUnderSustainedWrites(t *testing.T) {
	testCheckpointerSustainedWrites(t, false)
}

func TestCheckpointerRecoversAfterRepeatedSnapshots(t *testing.T) {
	testCheckpointerSustainedWrites(t, true)
}

func testCheckpointerSustainedWrites(t *testing.T, readers bool) {
	t.Helper()
	const (
		maxPages = 512
		txRows   = 64
		commits  = 300
	)
	db, file := openCheckpointed(t, sqlapi.SQLiteCheckpointConfig{Interval: 5 * time.Millisecond, MaxWALPages: maxPages})
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	// Readers keep pinning WAL snapshots, so the writer can rarely restart the
	// WAL from its beginning and PASSIVE checkpoints alone cannot bound it.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	if readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reader, err := Open("file:"+file+"?mode=ro&_busy_timeout=5000", defaultCheckpoint())
			if err != nil {
				t.Error(err)
				return
			}
			defer reader.Close()
			ctx := context.Background()
			for {
				select {
				case <-stop:
					return
				default:
				}
				tx, err := reader.BeginTx(ctx, nil)
				if err != nil {
					t.Error(err)
					return
				}
				var count int
				if err := tx.QueryRowContext(context.Background(), "SELECT count(*) FROM items").Scan(&count); err != nil {
					t.Error(err)
					_ = tx.Rollback()
					return
				}
				time.Sleep(10 * time.Millisecond)
				_ = tx.Rollback()
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}
	stopReaders := sync.OnceFunc(func() {
		close(stop)
		wg.Wait()
	})
	t.Cleanup(stopReaders)

	var peak int64
	for i := 0; i < commits; i++ {
		insertRows(t, db, txRows)
		peak = max(peak, sizeOf(t, file+"-wal"))
	}
	stopReaders()

	written := int64(commits * txRows * testPageSize)
	var rows int
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT count(*) FROM items").Scan(&rows))
	require.Equal(t, commits*txRows, rows, "all writes survive checkpoint contention")
	if readers {
		// Reader churn can repeatedly defer a bounded checkpoint attempt.
		// Once readers leave, the worker must truncate without another write.
		t.Logf("peak WAL with readers %d bytes, %d bytes written", peak, written)
		require.Eventually(t, func() bool { return sizeOf(t, file+"-wal") == 0 }, 10*time.Second, 10*time.Millisecond)
		return
	}
	// The WAL overshoots maxPages by the writes of one interval and one
	// checkpoint, which depends on scheduling; without escalation it grows
	// with the written volume.
	bound := written / 3
	t.Logf("peak WAL %d bytes, %d bytes written", peak, written)
	require.Less(t, peak, bound, "the WAL must not grow with the written volume")
}

func TestCloseStopsCheckpointerWaitingForWriter(t *testing.T) {
	db, file := openCheckpointed(t, sqlapi.SQLiteCheckpointConfig{Interval: time.Millisecond, MaxWALPages: 1})
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	// Hold the only writer connection while the worker requests its turn.
	conn, err := db.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	require.Eventually(t, func() bool { return db.Stats().WaitCount > 0 }, 5*time.Second, time.Millisecond)
	closed := make(chan error, 1)
	go func() { closed <- db.Close() }()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not stop the checkpointer waiting for the writer")
	}
	require.Zero(t, sizeOf(t, file+"-wal"), "shutdown checkpoints on the dedicated connection")
}

func TestCheckpointDefersForPinnedReaderWithoutHoldingWriter(t *testing.T) {
	file := filepath.Join(t.TempDir(), "app.db")
	c, err := NewConnector("file:"+file+"?_journal_mode=WAL&_busy_timeout=5000&_timeout=5000", sqlapi.SQLiteCheckpointConfig{Interval: time.Hour, MaxWALPages: 1})
	require.NoError(t, err)
	db := sql.OpenDB(c)
	db.SetMaxOpenConns(1)
	c.Attach(db)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ctx := context.Background()
	_, err = db.ExecContext(ctx, "CREATE TABLE items (id INTEGER PRIMARY KEY, value BLOB)")
	require.NoError(t, err)
	insertRows(t, db, 1)
	release := sync.OnceFunc(pinReader(t, file))
	t.Cleanup(release)
	insertRows(t, db, 100)
	done := make(chan error, 1)
	go func() { done <- c.checkpointer.checkpoint(ctx) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		release()
		<-done
		t.Fatal("checkpoint held the writer while waiting for a pinned reader")
	}
	// A busy checkpoint must release the writer turn and preserve its normal
	// busy policy for application queries.
	var busyTimeout int
	require.NoError(t, db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout))
	require.Equal(t, 5000, busyTimeout)
	insertRows(t, db, 1)
	release()
	require.NoError(t, c.checkpointer.checkpoint(ctx))
	require.Zero(t, sizeOf(t, file+"-wal"), "checkpoint retries after the reader releases")
}
