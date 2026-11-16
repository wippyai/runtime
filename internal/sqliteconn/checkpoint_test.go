// SPDX-License-Identifier: MPL-2.0

//go:build cgo

package sqliteconn

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rqlite/go-sqlite3"
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
	require.NoError(t, writeCheckpointRows(db, rows))
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
	const maxPages, seedRows, batchRows = 512, 1024, 64
	file := filepath.Join(t.TempDir(), "app.db")
	c, err := NewConnector("file:"+file+"?_journal_mode=WAL&_busy_timeout=5000", sqlapi.SQLiteCheckpointConfig{Interval: 5 * time.Millisecond, MaxWALPages: maxPages})
	require.NoError(t, err)
	db := sql.OpenDB(c)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.ExecContext(context.Background(), "CREATE TABLE items (id INTEGER PRIMARY KEY, value BLOB)")
	require.NoError(t, err)
	// Establish an oversized WAL before starting the worker. The later
	// below-threshold observation must therefore demonstrate actual progress.
	insertRows(t, db, seedRows)
	threshold := int64(walHeaderSize + maxPages*(walFrameHeaderSize+testPageSize))
	require.Greater(t, sizeOf(t, file+"-wal"), threshold)
	stop := make(chan struct{})
	var active atomic.Bool
	var commits atomic.Int64
	var wg sync.WaitGroup
	active.Store(true)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer active.Store(false)
		// Keep the load active until reclamation is observed, including when
		// storage is slow enough to outlast an arbitrary transaction count.
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := writeCheckpointRows(db, batchRows); err != nil {
				t.Errorf("write batch: %v", err)
				return
			}
			commits.Add(1)
		}
	}()
	stopWriter := sync.OnceFunc(func() { close(stop); wg.Wait() })
	t.Cleanup(stopWriter)
	c.Attach(db)
	defer func() {
		if t.Failed() {
			t.Logf("committed batches: %d; writer active: %t; WAL bytes: %d; threshold: %d", commits.Load(), active.Load(), sizeOf(t, file+"-wal"), threshold)
		}
	}()
	// The workload includes 128 durable commits as well as reclamation.
	// Leave headroom for slower storage and race-enabled Windows runners;
	// writer acquisition latency has its own independently bounded tests.
	require.Eventually(t, func() bool {
		return commits.Load() >= 128 && active.Load() && sizeOf(t, file+"-wal") <= threshold
	}, 30*time.Second, time.Millisecond, "checkpoint must make progress while the writer is still committing")
	stopWriter()
	var rows int64
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT count(*) FROM items").Scan(&rows))
	require.Equal(t, int64(seedRows)+commits.Load()*batchRows, rows)
}

func writeCheckpointRows(db *sql.DB, rows int) error {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for range rows {
		if _, err := tx.ExecContext(context.Background(), "INSERT INTO items (value) VALUES (zeroblob(3000))"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func TestCheckpointerRecoversAfterRepeatedSnapshots(t *testing.T) {
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
	// Reader churn can repeatedly defer a bounded checkpoint attempt.
	// Once readers leave, the worker must return below its threshold without
	// another write. PASSIVE can retain a non-empty WAL below the threshold.
	t.Logf("peak WAL with readers %d bytes, %d bytes written", peak, written)
	threshold := int64(walHeaderSize + maxPages*(walFrameHeaderSize+testPageSize))
	require.Eventually(t, func() bool { return sizeOf(t, file+"-wal") <= threshold }, 10*time.Second, 10*time.Millisecond)
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

func TestCheckpointWriteAcquisitionWithLargeWAL(t *testing.T) {
	for _, scenario := range []string{"reader_released_after_passive", "commit_after_passive", "cancel_during_backfill"} {
		t.Run(scenario, func(t *testing.T) {
			const rows = 8192 // About 32 MiB, independent of machine speed.
			appCtx := context.Background()
			ctx, cancel := context.WithCancel(appCtx)
			defer cancel()
			file := filepath.Join(t.TempDir(), "app.db")
			c, err := NewConnector("file:"+file+"?_journal_mode=WAL", sqlapi.SQLiteCheckpointConfig{Interval: time.Hour, MaxWALPages: 256})
			require.NoError(t, err)
			db := sql.OpenDB(c)
			db.SetMaxOpenConns(1)
			c.Attach(db)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			_, err = db.ExecContext(ctx, "CREATE TABLE items (id INTEGER PRIMARY KEY, value BLOB)")
			require.NoError(t, err)
			insertRows(t, db, rows)
			releaseReader := sync.OnceFunc(pinReader(t, file))
			t.Cleanup(releaseReader)
			if scenario != "commit_after_passive" {
				insertRows(t, db, rows)
			}

			// Delay backfill at SQLite's authorizer boundary to represent slow
			// storage deterministically, rather than relying on disk throughput.
			// PASSIVE and the actual checkpoint SQL still run against the real WAL.
			checkpointing := make(chan struct{}, 1)
			resume := make(chan struct{})
			resumeCheckpoint := sync.OnceFunc(func() { close(resume) })
			t.Cleanup(resumeCheckpoint)
			var slow atomic.Bool
			require.NoError(t, c.checkpointer.own.Close())
			maintenance := newDriver(0)
			hook := maintenance.ConnectHook
			maintenance.ConnectHook = func(conn *sqlite3.SQLiteConn) error {
				if err := hook(conn); err != nil {
					return err
				}
				conn.RegisterAuthorizer(func(op int, pragma, mode, _ string) int {
					if op == sqlite3.SQLITE_PRAGMA && pragma == "wal_checkpoint" && (mode == "PASSIVE" || mode == "TRUNCATE") && slow.CompareAndSwap(true, false) {
						checkpointing <- struct{}{}
						<-resume
					}
					return sqlite3.SQLITE_OK
				})
				return nil
			}
			c.checkpointer.own = sql.OpenDB(&Connector{driver: maintenance, dsn: c.dsn})
			c.checkpointer.own.SetMaxOpenConns(1)

			// Queue maintenance behind an application turn. The old code has
			// already run PASSIVE here; a large tail can remain or arrive before
			// it borrows the connection for TRUNCATE.
			conn, err := db.Conn(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			waits := db.Stats().WaitCount
			done := make(chan error, 1)
			go func() { done <- c.checkpointer.checkpoint(ctx) }()
			require.Eventually(t, func() bool { return db.Stats().WaitCount > waits }, 5*time.Second, time.Millisecond)
			if scenario == "commit_after_passive" {
				tx, err := conn.BeginTx(ctx, nil)
				require.NoError(t, err)
				_, err = tx.ExecContext(ctx, "WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i < ?) INSERT INTO items(value) SELECT zeroblob(3000) FROM n", rows)
				require.NoError(t, err)
				require.NoError(t, tx.Commit())
			}
			walSize := sizeOf(t, file+"-wal")
			require.Greater(t, walSize, int64(rows*testPageSize))
			require.Less(t, walSize, int64(80<<20), "fixture must remain bounded")
			releaseReader()
			slow.Store(true)
			require.NoError(t, conn.Close())
			select {
			case <-checkpointing:
				time.AfterFunc(500*time.Millisecond, resumeCheckpoint)
			case err := <-done:
				t.Fatalf("checkpoint finished before backfilling the tail: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("checkpoint did not start backfilling the tail")
			}
			if scenario == "cancel_during_backfill" {
				cancel()
			}

			start := time.Now()
			tx, err := db.BeginTx(appCtx, nil)
			acquisition := time.Since(start)
			require.NoError(t, err)
			_, err = tx.ExecContext(appCtx, "INSERT INTO items(value) VALUES ('foreground')")
			require.NoError(t, err)
			require.NoError(t, tx.Commit())
			if scenario == "cancel_during_backfill" {
				require.ErrorIs(t, <-done, context.Canceled)
			} else {
				require.NoError(t, <-done)
			}
			slow.Store(false)
			t.Logf("WAL %d bytes; application BeginTx acquisition %s", walSize, acquisition)
			require.Less(t, acquisition, 250*time.Millisecond, "checkpoint must not reserve the writer for backlog storage work")
			require.NoError(t, c.checkpointer.checkpoint(appCtx))
			require.Zero(t, sizeOf(t, file+"-wal"), "later PASSIVE and reset must reclaim the WAL")
		})
	}
}

func TestAttachedCheckpointWriteAcquisitionWithLargeWAL(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	c, err := NewConnector("file:"+filepath.Join(dir, "app.db")+"?_journal_mode=WAL", sqlapi.SQLiteCheckpointConfig{Interval: time.Hour, MaxWALPages: 1 << 20})
	require.NoError(t, err)
	checkpointing := make(chan struct{}, 1)
	resume := make(chan struct{})
	resumeCheckpoint := sync.OnceFunc(func() { close(resume) })
	hook := c.driver.ConnectHook
	c.driver.ConnectHook = func(conn *sqlite3.SQLiteConn) error {
		if err := hook(conn); err != nil {
			return err
		}
		conn.RegisterAuthorizer(func(op int, pragma, mode, schema string) int {
			if op == sqlite3.SQLITE_PRAGMA && pragma == "wal_checkpoint" && mode == "PASSIVE" && schema == "aux" {
				checkpointing <- struct{}{}
				<-resume
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	}
	db := sql.OpenDB(c)
	db.SetMaxOpenConns(1)
	c.Attach(db)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	t.Cleanup(resumeCheckpoint)
	_, err = db.ExecContext(ctx, "CREATE TABLE items (id INTEGER PRIMARY KEY, value BLOB)")
	require.NoError(t, err)
	file := filepath.Join(dir, "aux #.db")
	_, err = db.ExecContext(ctx, "ATTACH DATABASE ? AS aux", file)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "PRAGMA aux.journal_mode=WAL")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "CREATE TABLE aux.items (value BLOB)")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i < 8192) INSERT INTO aux.items SELECT zeroblob(3000) FROM n")
	require.NoError(t, err)
	require.Greater(t, sizeOf(t, file+"-wal"), int64(32<<20))
	require.Less(t, sizeOf(t, file+"-wal"), int64(40<<20))
	done := make(chan error, 1)
	go func() { done <- c.checkpointer.checkpoint(ctx) }()
	select {
	case <-checkpointing:
	case <-time.After(5 * time.Second):
		t.Fatal("attached checkpoint did not start")
	}
	time.AfterFunc(500*time.Millisecond, resumeCheckpoint)
	start := time.Now()
	tx, err := db.BeginTx(ctx, nil)
	acquisition := time.Since(start)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "INSERT INTO items(value) VALUES ('foreground')")
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.NoError(t, <-done)
	t.Logf("application BeginTx acquisition during attached backfill: %s", acquisition)
	require.Less(t, acquisition, 250*time.Millisecond, "attached storage work must run outside the application writer turn")
	require.Greater(t, sizeOf(t, file), int64(32<<20))
}
