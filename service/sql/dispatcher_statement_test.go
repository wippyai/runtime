// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

func TestD14PreparedQueryColumnProjection(t *testing.T) {
	db := newBoundaryDB(t)
	stmt, err := db.PrepareContext(t.Context(), `SELECT ? AS literal_text, CAST(? AS INTEGER) AS literal_number, CAST(? AS BLOB) AS literal_bytes`)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stmt.Close()) })

	response := executeStmtQuery(t.Context(), stmt, []any{"text", "42", "bytes"})
	require.NoError(t, response.Error)
	require.Equal(t, []string{"literal_text", "literal_number", "literal_bytes"}, response.Columns)
	require.Equal(t, [][]any{{"text", int64(42), []byte("bytes")}}, response.Rows)
}

func TestD15TransactionPreparedRollback(t *testing.T) {
	db := newBoundaryDB(t)
	tx, err := db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	handlers := boundaryHandlers()

	prepareResults := make(boundaryDispatchReceiver, 1)
	err = handlers[sqlapi.TxPrepare].Handle(ctx, &sqlapi.TxPrepareCmd{
		Tx: tx, Query: `INSERT INTO entries (value) VALUES (?)`,
	}, 1, prepareResults)
	require.NoError(t, err)
	prepared := awaitBoundaryResult(t, ctx, prepareResults)
	require.NoError(t, prepared.err)
	prepareResponse := prepared.data.(sqlapi.PrepareResponse)
	require.NoError(t, prepareResponse.Error)
	require.NotNil(t, prepareResponse.Stmt)
	t.Cleanup(func() { _ = prepareResponse.Stmt.Close() })

	executeResults := make(boundaryDispatchReceiver, 1)
	err = handlers[sqlapi.StmtExecute].Handle(ctx, &sqlapi.StmtExecuteCmd{
		Stmt: prepareResponse.Stmt, Params: []any{"pending"},
	}, 2, executeResults)
	require.NoError(t, err)
	executed := awaitBoundaryResult(t, ctx, executeResults)
	require.NoError(t, executed.err)
	executeResponse := executed.data.(sqlapi.ExecuteResponse)
	require.NoError(t, executeResponse.Error)
	require.EqualValues(t, 1, executeResponse.RowsAffected)

	rollbackResults := make(boundaryDispatchReceiver, 1)
	err = handlers[sqlapi.TxRollback].Handle(ctx, &sqlapi.TxRollbackCmd{Tx: tx}, 3, rollbackResults)
	require.NoError(t, err)
	rolledBack := awaitBoundaryResult(t, ctx, rollbackResults)
	require.NoError(t, rolledBack.err)

	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE value = 'pending'`).Scan(&count))
	require.Zero(t, count)
}
