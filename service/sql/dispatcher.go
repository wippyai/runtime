// Package sql provides SQL command handlers for the dispatcher system.
package sql

import (
	"context"
	"database/sql"

	"github.com/wippyai/runtime/api/dispatcher"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

// Dispatcher handles SQL commands using a stateless goroutine pattern.
//
// Each command spawns a goroutine that executes the SQL operation and
// delivers results via the receiver. Goroutine lifecycle is tied to context:
// when context is cancelled, operations check ctx.Err() and skip result
// delivery, allowing natural termination.
//
// Resource cleanup is handled by the Store layer (Store.Close releases
// connections) and FrameContext cleanup (commits/rollbacks transactions).
// This pattern is consistent with other stateless dispatchers in the system
// (contract, function) where explicit goroutine tracking isn't needed.
type Dispatcher struct{}

// NewDispatcher creates a new SQL dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{}
}

// Start is a no-op since this dispatcher has no background workers.
func (d *Dispatcher) Start(_ context.Context) error {
	return nil
}

// Stop is a no-op since goroutine cleanup is handled via context cancellation.
func (d *Dispatcher) Stop(_ context.Context) error {
	return nil
}

// RegisterAll registers all SQL handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(sqlapi.Query, dispatcher.HandlerFunc(d.handleQuery))
	register(sqlapi.Execute, dispatcher.HandlerFunc(d.handleExecute))
	register(sqlapi.Prepare, dispatcher.HandlerFunc(d.handlePrepare))
	register(sqlapi.Begin, dispatcher.HandlerFunc(d.handleBegin))
	register(sqlapi.StmtQuery, dispatcher.HandlerFunc(d.handleStmtQuery))
	register(sqlapi.StmtExecute, dispatcher.HandlerFunc(d.handleStmtExecute))
	register(sqlapi.StmtClose, dispatcher.HandlerFunc(d.handleStmtClose))
	register(sqlapi.TxQuery, dispatcher.HandlerFunc(d.handleTxQuery))
	register(sqlapi.TxExecute, dispatcher.HandlerFunc(d.handleTxExecute))
	register(sqlapi.TxPrepare, dispatcher.HandlerFunc(d.handleTxPrepare))
	register(sqlapi.TxCommit, dispatcher.HandlerFunc(d.handleTxCommit))
	register(sqlapi.TxRollback, dispatcher.HandlerFunc(d.handleTxRollback))
}

func (d *Dispatcher) handleQuery(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	qc := cmd.(*sqlapi.QueryCmd)
	go func() {
		resp := executeQuery(ctx, qc.DB, qc.Query, qc.Params)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, resp, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handleExecute(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	ec := cmd.(*sqlapi.ExecuteCmd)
	go func() {
		resp := executeExec(ctx, ec.DB, ec.Query, ec.Params)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, resp, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handlePrepare(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	pc := cmd.(*sqlapi.PrepareCmd)
	go func() {
		stmt, err := pc.DB.PrepareContext(ctx, pc.Query)
		if ctx.Err() != nil {
			if stmt != nil {
				_ = stmt.Close()
			}
			return
		}
		receiver.CompleteYield(tag, sqlapi.PrepareResponse{Stmt: stmt, Error: err}, nil)
	}()
	return nil
}

func (d *Dispatcher) handleBegin(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	bc := cmd.(*sqlapi.BeginCmd)
	go func() {
		tx, err := bc.DB.BeginTx(ctx, bc.Options)
		if ctx.Err() != nil {
			if tx != nil {
				_ = tx.Rollback()
			}
			return
		}
		receiver.CompleteYield(tag, sqlapi.BeginResponse{Tx: tx, Error: err}, nil)
	}()
	return nil
}

func (d *Dispatcher) handleStmtQuery(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	sc := cmd.(*sqlapi.StmtQueryCmd)
	go func() {
		resp := executeStmtQuery(ctx, sc.Stmt, sc.Params)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, resp, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handleStmtExecute(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	sc := cmd.(*sqlapi.StmtExecuteCmd)
	go func() {
		resp := executeStmtExec(ctx, sc.Stmt, sc.Params)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, resp, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handleStmtClose(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	sc := cmd.(*sqlapi.StmtCloseCmd)
	go func() {
		err := sc.Stmt.Close()
		receiver.CompleteYield(tag, nil, err)
	}()
	return nil
}

func (d *Dispatcher) handleTxQuery(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	tc := cmd.(*sqlapi.TxQueryCmd)
	go func() {
		resp := executeTxQuery(ctx, tc.Tx, tc.Query, tc.Params)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, resp, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handleTxExecute(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	tc := cmd.(*sqlapi.TxExecuteCmd)
	go func() {
		resp := executeTxExec(ctx, tc.Tx, tc.Query, tc.Params)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, resp, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handleTxPrepare(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	tc := cmd.(*sqlapi.TxPrepareCmd)
	go func() {
		stmt, err := tc.Tx.PrepareContext(ctx, tc.Query)
		if ctx.Err() != nil {
			if stmt != nil {
				_ = stmt.Close()
			}
			return
		}
		receiver.CompleteYield(tag, sqlapi.PrepareResponse{Stmt: stmt, Error: err}, nil)
	}()
	return nil
}

func (d *Dispatcher) handleTxCommit(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	tc := cmd.(*sqlapi.TxCommitCmd)
	go func() {
		err := tc.Tx.Commit()
		receiver.CompleteYield(tag, nil, err)
	}()
	return nil
}

func (d *Dispatcher) handleTxRollback(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	tc := cmd.(*sqlapi.TxRollbackCmd)
	go func() {
		err := tc.Tx.Rollback()
		receiver.CompleteYield(tag, nil, err)
	}()
	return nil
}

// executeQuery runs a query and scans all rows into the response.
func executeQuery(ctx context.Context, db *sql.DB, query string, params []any) sqlapi.QueryResponse {
	rows, err := db.QueryContext(ctx, query, params...)
	if err != nil {
		return sqlapi.QueryResponse{Error: err}
	}
	defer func() { _ = rows.Close() }()

	return scanRows(rows)
}

// buildExecResponse creates ExecuteResponse from sql.Result.
func buildExecResponse(result sql.Result, err error) sqlapi.ExecuteResponse {
	if err != nil {
		return sqlapi.ExecuteResponse{Error: err}
	}
	lastID, _ := result.LastInsertId()
	affected, _ := result.RowsAffected()
	return sqlapi.ExecuteResponse{LastInsertID: lastID, RowsAffected: affected}
}

// executeExec runs an exec statement and returns the result.
func executeExec(ctx context.Context, db *sql.DB, query string, params []any) sqlapi.ExecuteResponse {
	return buildExecResponse(db.ExecContext(ctx, query, params...))
}

// executeStmtQuery runs a prepared statement query.
func executeStmtQuery(ctx context.Context, stmt *sql.Stmt, params []any) sqlapi.QueryResponse {
	rows, err := stmt.QueryContext(ctx, params...)
	if err != nil {
		return sqlapi.QueryResponse{Error: err}
	}
	defer func() { _ = rows.Close() }()
	return scanRows(rows)
}

// executeStmtExec runs a prepared statement exec.
func executeStmtExec(ctx context.Context, stmt *sql.Stmt, params []any) sqlapi.ExecuteResponse {
	return buildExecResponse(stmt.ExecContext(ctx, params...))
}

// executeTxQuery runs a query within a transaction.
func executeTxQuery(ctx context.Context, tx *sql.Tx, query string, params []any) sqlapi.QueryResponse {
	rows, err := tx.QueryContext(ctx, query, params...)
	if err != nil {
		return sqlapi.QueryResponse{Error: err}
	}
	defer func() { _ = rows.Close() }()
	return scanRows(rows)
}

// executeTxExec runs an exec within a transaction.
func executeTxExec(ctx context.Context, tx *sql.Tx, query string, params []any) sqlapi.ExecuteResponse {
	return buildExecResponse(tx.ExecContext(ctx, query, params...))
}

// scanRows scans all rows into the response.
func scanRows(rows *sql.Rows) sqlapi.QueryResponse {
	cols, err := rows.Columns()
	if err != nil {
		return sqlapi.QueryResponse{Error: err}
	}

	var result [][]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}

		if err := rows.Scan(ptrs...); err != nil {
			return sqlapi.QueryResponse{Error: err}
		}

		result = append(result, values)
	}

	if err := rows.Err(); err != nil {
		return sqlapi.QueryResponse{Error: err}
	}

	return sqlapi.QueryResponse{Columns: cols, Rows: result}
}
