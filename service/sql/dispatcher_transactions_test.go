// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

type boundaryDispatchResult struct {
	data any
	err  error
}

type boundaryDispatchReceiver chan boundaryDispatchResult

func (r boundaryDispatchReceiver) CompleteYield(_ uint64, data any, err error) {
	r <- boundaryDispatchResult{data: data, err: err}
}

func boundaryHandlers() map[dispatcher.CommandID]dispatcher.Handler {
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	NewDispatcher().RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) { handlers[id] = h })
	return handlers
}

func awaitBoundaryResult(ctx context.Context, t *testing.T, results <-chan boundaryDispatchResult) boundaryDispatchResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-ctx.Done():
		t.Fatalf("dispatcher did not complete: %v", ctx.Err())
		return boundaryDispatchResult{}
	}
}

func newBoundaryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.ExecContext(t.Context(), `CREATE TABLE entries (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func TestD12DispatcherRollbackRemovesWrite(t *testing.T) {
	db := newBoundaryDB(t)
	tx, err := db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `INSERT INTO entries (value) VALUES ('pending')`)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	results := make(boundaryDispatchReceiver, 1)
	err = boundaryHandlers()[sqlapi.TxRollback].Handle(ctx, &sqlapi.TxRollbackCmd{Tx: tx}, 1, results)
	require.NoError(t, err)
	result := awaitBoundaryResult(ctx, t, results)
	require.NoError(t, result.err)
	require.Nil(t, result.data, "rollback must not fabricate success metadata")

	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE value = 'pending'`).Scan(&count))
	require.Zero(t, count)
}

func TestD13TransactionQueryIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "isolation.db")
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(path)+"?_busy_timeout=5000")
	require.NoError(t, err)
	db.SetMaxOpenConns(2)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var journalMode string
	require.NoError(t, db.QueryRowContext(ctx, `PRAGMA journal_mode=WAL`).Scan(&journalMode))
	require.Equal(t, "wal", journalMode)
	_, err = db.ExecContext(ctx, `CREATE TABLE entries (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO entries (value) VALUES ('stable')`)
	require.NoError(t, err)

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.ExecContext(ctx, `INSERT INTO entries (value) VALUES ('pending')`)
	require.NoError(t, err)

	results := make(boundaryDispatchReceiver, 1)
	err = boundaryHandlers()[sqlapi.TxQuery].Handle(ctx, &sqlapi.TxQueryCmd{
		Tx: tx, Query: `SELECT COUNT(*) FROM entries`,
	}, 1, results)
	require.NoError(t, err)
	result := awaitBoundaryResult(ctx, t, results)
	require.NoError(t, result.err)
	inside := result.data.(sqlapi.QueryResponse)
	require.NoError(t, inside.Error)
	require.Equal(t, [][]any{{int64(2)}}, inside.Rows)

	var outside int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries`).Scan(&outside))
	require.Equal(t, 1, outside, "outside connection must retain the pre-transaction snapshot")
}
