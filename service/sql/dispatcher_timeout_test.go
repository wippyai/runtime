// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

const endlessCount = `WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) SELECT max(x) FROM c`

func responseError(t *testing.T, data any) error {
	t.Helper()
	switch resp := data.(type) {
	case sqlapi.QueryResponse:
		return resp.Error
	case sqlapi.ExecuteResponse:
		return resp.Error
	}
	t.Fatalf("unexpected response %T", data)
	return nil
}

func TestDispatcherStatementTimeout(t *testing.T) {
	const timeout = 100 * time.Millisecond
	cases := map[string]func(*sql.DB, *sql.Tx, *sql.Stmt) dispatcher.Command{
		"query": func(db *sql.DB, _ *sql.Tx, _ *sql.Stmt) dispatcher.Command {
			return &sqlapi.QueryCmd{DB: db, Query: endlessCount, Timeout: timeout}
		},
		"execute": func(db *sql.DB, _ *sql.Tx, _ *sql.Stmt) dispatcher.Command {
			return &sqlapi.ExecuteCmd{DB: db, Query: endlessCount, Timeout: timeout}
		},
		"statement query": func(_ *sql.DB, _ *sql.Tx, stmt *sql.Stmt) dispatcher.Command {
			return &sqlapi.StmtQueryCmd{Stmt: stmt, Timeout: timeout}
		},
		"statement execute": func(_ *sql.DB, _ *sql.Tx, stmt *sql.Stmt) dispatcher.Command {
			return &sqlapi.StmtExecuteCmd{Stmt: stmt, Timeout: timeout}
		},
		"tx query": func(_ *sql.DB, tx *sql.Tx, _ *sql.Stmt) dispatcher.Command {
			return &sqlapi.TxQueryCmd{Tx: tx, Query: endlessCount, Timeout: timeout}
		},
		"tx execute": func(_ *sql.DB, tx *sql.Tx, _ *sql.Stmt) dispatcher.Command {
			return &sqlapi.TxExecuteCmd{Tx: tx, Query: endlessCount, Timeout: timeout}
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			db, err := sql.Open("sqlite3", ":memory:")
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			var tx *sql.Tx
			var stmt *sql.Stmt
			switch name {
			case "tx query", "tx execute":
				tx, err = db.BeginTx(ctx, nil)
				require.NoError(t, err)
				defer func() { require.NoError(t, tx.Rollback()) }()
			case "statement query", "statement execute":
				stmt, err = db.PrepareContext(ctx, endlessCount)
				require.NoError(t, err)
				defer func() { require.NoError(t, stmt.Close()) }()
			}

			cmd := build(db, tx, stmt)
			results := make(boundaryDispatchReceiver, 1)
			start := time.Now()
			require.NoError(t, boundaryHandlers()[cmd.CmdID()].Handle(ctx, cmd, 1, results))
			result := awaitBoundaryResult(ctx, t, results)
			require.Less(t, time.Since(start), 3*time.Second)
			require.NoError(t, result.err)

			statementErr := responseError(t, result.data)
			require.ErrorIs(t, statementErr, context.DeadlineExceeded)
			var timeoutErr *sqlapi.StatementTimeoutError
			require.True(t, errors.As(statementErr, &timeoutErr), "error %v is not a statement timeout", statementErr)
			require.Equal(t, timeout, timeoutErr.Timeout)
		})
	}
}

func TestDispatcherStatementTimeoutLeavesFastStatements(t *testing.T) {
	db := newBoundaryDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	handlers := boundaryHandlers()

	results := make(boundaryDispatchReceiver, 1)
	require.NoError(t, handlers[sqlapi.Execute].Handle(ctx, &sqlapi.ExecuteCmd{
		DB: db, Query: `INSERT INTO entries (value) VALUES (?)`, Params: []any{"kept"}, Timeout: time.Second,
	}, 1, results))
	exec := awaitBoundaryResult(ctx, t, results)
	require.NoError(t, exec.err)
	require.NoError(t, exec.data.(sqlapi.ExecuteResponse).Error)

	require.NoError(t, handlers[sqlapi.Query].Handle(ctx, &sqlapi.QueryCmd{
		DB: db, Query: `SELECT value FROM entries`, Timeout: time.Second,
	}, 2, results))
	query := awaitBoundaryResult(ctx, t, results)
	require.NoError(t, query.err)
	resp := query.data.(sqlapi.QueryResponse)
	require.NoError(t, resp.Error)
	require.Equal(t, [][]any{{"kept"}}, resp.Rows)
}
