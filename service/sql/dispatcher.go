// Package sql provides SQL command handlers for the dispatcher system.
package sql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/wippyai/runtime/api/dispatcher"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

// Dispatcher handles SQL commands.
type Dispatcher struct{}

// NewDispatcher creates a new SQL dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{}
}

// Start is a no-op for SQL dispatcher.
func (d *Dispatcher) Start(_ context.Context) error {
	return nil
}

// Stop is a no-op for SQL dispatcher.
func (d *Dispatcher) Stop(_ context.Context) error {
	return nil
}

// RegisterAll registers all SQL handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(sqlapi.CmdQuery, dispatcher.HandlerFunc(d.handleQuery))
	register(sqlapi.CmdExecute, dispatcher.HandlerFunc(d.handleExecute))
	register(sqlapi.CmdPrepare, dispatcher.HandlerFunc(d.handlePrepare))
	register(sqlapi.CmdBegin, dispatcher.HandlerFunc(d.handleBegin))
	register(sqlapi.CmdStmtQuery, dispatcher.HandlerFunc(d.handleStmtQuery))
	register(sqlapi.CmdStmtExecute, dispatcher.HandlerFunc(d.handleStmtExecute))
	register(sqlapi.CmdStmtClose, dispatcher.HandlerFunc(d.handleStmtClose))
	register(sqlapi.CmdTxQuery, dispatcher.HandlerFunc(d.handleTxQuery))
	register(sqlapi.CmdTxExecute, dispatcher.HandlerFunc(d.handleTxExecute))
	register(sqlapi.CmdTxPrepare, dispatcher.HandlerFunc(d.handleTxPrepare))
	register(sqlapi.CmdTxCommit, dispatcher.HandlerFunc(d.handleTxCommit))
	register(sqlapi.CmdTxRollback, dispatcher.HandlerFunc(d.handleTxRollback))
}

func (d *Dispatcher) handleQuery(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	qc := cmd.(*sqlapi.QueryCmd)
	fmt.Printf("[SQL] handleQuery tag=%d, query=%s\n", tag, qc.Query)
	go func() {
		fmt.Printf("[SQL] executing query tag=%d\n", tag)
		resp := executeQuery(ctx, qc.DB, qc.Query, qc.Params)
		fmt.Printf("[SQL] query done tag=%d, err=%v, ctx.Err=%v\n", tag, resp.Error, ctx.Err())
		if ctx.Err() == nil {
			fmt.Printf("[SQL] calling CompleteYield tag=%d\n", tag)
			receiver.CompleteYield(tag, resp, nil)
			fmt.Printf("[SQL] CompleteYield returned tag=%d\n", tag)
		} else {
			fmt.Printf("[SQL] ctx cancelled, not calling CompleteYield tag=%d\n", tag)
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
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, sqlapi.PrepareResponse{Stmt: stmt, Error: err}, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handleBegin(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	bc := cmd.(*sqlapi.BeginCmd)
	go func() {
		tx, err := bc.DB.BeginTx(ctx, bc.Options)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, sqlapi.BeginResponse{Tx: tx, Error: err}, nil)
		}
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
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, sqlapi.PrepareResponse{Stmt: stmt, Error: err}, nil)
		}
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
	defer rows.Close()

	return scanRows(rows)
}

// executeExec runs an exec statement and returns the result.
func executeExec(ctx context.Context, db *sql.DB, query string, params []any) sqlapi.ExecuteResponse {
	result, err := db.ExecContext(ctx, query, params...)
	if err != nil {
		return sqlapi.ExecuteResponse{Error: err}
	}

	lastID, _ := result.LastInsertId()
	affected, _ := result.RowsAffected()
	return sqlapi.ExecuteResponse{LastInsertID: lastID, RowsAffected: affected}
}

// executeStmtQuery runs a prepared statement query.
func executeStmtQuery(ctx context.Context, stmt *sql.Stmt, params []any) sqlapi.QueryResponse {
	rows, err := stmt.QueryContext(ctx, params...)
	if err != nil {
		return sqlapi.QueryResponse{Error: err}
	}
	defer rows.Close()

	return scanRows(rows)
}

// executeStmtExec runs a prepared statement exec.
func executeStmtExec(ctx context.Context, stmt *sql.Stmt, params []any) sqlapi.ExecuteResponse {
	result, err := stmt.ExecContext(ctx, params...)
	if err != nil {
		return sqlapi.ExecuteResponse{Error: err}
	}

	lastID, _ := result.LastInsertId()
	affected, _ := result.RowsAffected()
	return sqlapi.ExecuteResponse{LastInsertID: lastID, RowsAffected: affected}
}

// executeTxQuery runs a query within a transaction.
func executeTxQuery(ctx context.Context, tx *sql.Tx, query string, params []any) sqlapi.QueryResponse {
	rows, err := tx.QueryContext(ctx, query, params...)
	if err != nil {
		return sqlapi.QueryResponse{Error: err}
	}
	defer rows.Close()

	return scanRows(rows)
}

// executeTxExec runs an exec within a transaction.
func executeTxExec(ctx context.Context, tx *sql.Tx, query string, params []any) sqlapi.ExecuteResponse {
	result, err := tx.ExecContext(ctx, query, params...)
	if err != nil {
		return sqlapi.ExecuteResponse{Error: err}
	}

	lastID, _ := result.LastInsertId()
	affected, _ := result.RowsAffected()
	return sqlapi.ExecuteResponse{LastInsertID: lastID, RowsAffected: affected}
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
