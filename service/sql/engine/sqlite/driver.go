// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"sync"

	"github.com/mattn/go-sqlite3"
)

// observedConn delegates all normal SQL behavior while ensuring every physical
// connection operation gets a post-operation finalization point.
type observedConn struct {
	raw     driver.Conn
	sqlite  *sqlite3.SQLiteConn
	backend *sqliteBackend
	state   *sqliteConnectionState
}

func (c *observedConn) bindIfActive() {
	c.state.bind(c.sqlite)
}

func (c *observedConn) Prepare(query string) (driver.Stmt, error) {
	c.state.prepareMeta = statementMeta{}
	stmt, err := c.raw.Prepare(query)
	meta := c.state.prepareMeta
	c.state.prepareMeta = statementMeta{}
	if err != nil {
		return nil, err
	}
	return &observedStmt{raw: stmt, conn: c, query: query, meta: meta}, nil
}

func (c *observedConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	preparer, ok := c.raw.(driver.ConnPrepareContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.state.prepareMeta = statementMeta{}
	stmt, err := preparer.PrepareContext(ctx, query)
	meta := c.state.prepareMeta
	c.state.prepareMeta = statementMeta{}
	if err != nil {
		return nil, err
	}
	return &observedStmt{raw: stmt, conn: c, query: query, meta: meta}, nil
}

func (c *observedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := c.raw.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.state.statementBegin(query)
	result, err := execer.ExecContext(ctx, query, args)
	c.state.statementEnd(query, err)
	return result, err
}

func (c *observedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := c.raw.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.state.statementBegin(query)
	rows, err := queryer.QueryContext(ctx, query, args)
	if err != nil {
		c.state.statementEnd(query, err)
		return nil, err
	}
	return &observedRows{raw: rows, conn: c, query: query}, nil
}

func (c *observedConn) CheckNamedValue(value *driver.NamedValue) error {
	checker, ok := c.raw.(driver.NamedValueChecker)
	if !ok {
		return driver.ErrSkip
	}
	return checker.CheckNamedValue(value)
}

func (c *observedConn) Ping(ctx context.Context) error {
	pinger, ok := c.raw.(driver.Pinger)
	if !ok {
		return nil
	}
	return pinger.Ping(ctx)
}

func (c *observedConn) Close() error {
	c.state.rollback()
	return c.raw.Close()
}

func (c *observedConn) Begin() (driver.Tx, error) {
	//nolint:staticcheck // driver.Conn requires Begin for legacy driver compatibility.
	tx, err := c.raw.Begin()
	if err != nil {
		return nil, err
	}
	return &observedTx{raw: tx, conn: c}, nil
}

func (c *observedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	begin, ok := c.raw.(driver.ConnBeginTx)
	if !ok {
		return c.Begin()
	}
	tx, err := begin.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &observedTx{raw: tx, conn: c}, nil
}

type observedTx struct {
	raw  driver.Tx
	conn *observedConn
}

func (t *observedTx) Commit() error {
	err := t.raw.Commit()
	if t.conn.state.commitPending {
		t.conn.state.finalizeAfterError("COMMIT", err)
	}
	return err
}

func (t *observedTx) Rollback() error {
	err := t.raw.Rollback()
	t.conn.state.rollback()
	return err
}

type observedStmt struct {
	raw   driver.Stmt
	conn  *observedConn
	query string
	meta  statementMeta
}

func (s *observedStmt) Close() error  { return s.raw.Close() }
func (s *observedStmt) NumInput() int { return s.raw.NumInput() }

func (s *observedStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := s.raw.(driver.StmtExecContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	s.conn.state.statementBeginWithMeta(s.meta)
	result, err := execer.ExecContext(ctx, args)
	s.conn.state.statementEndWithMeta(err, s.meta)
	return result, err
}

func (s *observedStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := s.raw.(driver.StmtQueryContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	s.conn.state.statementBeginWithMeta(s.meta)
	rows, err := queryer.QueryContext(ctx, args)
	if err != nil {
		s.conn.state.statementEndWithMeta(err, s.meta)
		return nil, err
	}
	return &observedRows{raw: rows, conn: s.conn, query: s.query, meta: s.meta}, nil
}

func (s *observedStmt) ColumnConverter(index int) driver.ValueConverter {
	//nolint:staticcheck // Preserve the optional legacy converter exposed by the wrapped driver.
	if converter, ok := s.raw.(driver.ColumnConverter); ok {
		return converter.ColumnConverter(index)
	}
	return driver.DefaultParameterConverter
}

func (s *observedStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.conn.state.statementBeginWithMeta(s.meta)
	//nolint:staticcheck // driver.Stmt requires Exec for legacy driver compatibility.
	result, err := s.raw.Exec(args)
	s.conn.state.statementEndWithMeta(err, s.meta)
	return result, err
}

func (s *observedStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.conn.state.statementBeginWithMeta(s.meta)
	//nolint:staticcheck // driver.Stmt requires Query for legacy driver compatibility.
	rows, err := s.raw.Query(args)
	if err != nil {
		s.conn.state.statementEndWithMeta(err, s.meta)
		return nil, err
	}
	return &observedRows{raw: rows, conn: s.conn, query: s.query, meta: s.meta}, nil
}

type observedRows struct {
	raw    driver.Rows
	conn   *observedConn
	query  string
	meta   statementMeta
	closed bool
	mu     sync.Mutex
}

func (r *observedRows) Columns() []string { return r.raw.Columns() }

func (r *observedRows) Next(dest []driver.Value) error {
	err := r.raw.Next(dest)
	if errors.Is(err, io.EOF) || err != nil {
		r.finish(err)
	}
	return err
}

func (r *observedRows) Close() error {
	err := r.raw.Close()
	r.finish(err)
	return err
}

func (r *observedRows) ColumnTypeDatabaseTypeName(index int) string {
	if rows, ok := r.raw.(driver.RowsColumnTypeDatabaseTypeName); ok {
		return rows.ColumnTypeDatabaseTypeName(index)
	}
	return ""
}

func (r *observedRows) ColumnTypeLength(index int) (int64, bool) {
	if rows, ok := r.raw.(driver.RowsColumnTypeLength); ok {
		return rows.ColumnTypeLength(index)
	}
	return 0, false
}

func (r *observedRows) ColumnTypeNullable(index int) (bool, bool) {
	if rows, ok := r.raw.(driver.RowsColumnTypeNullable); ok {
		return rows.ColumnTypeNullable(index)
	}
	return false, false
}

func (r *observedRows) ColumnTypePrecisionScale(index int) (int64, int64, bool) {
	if rows, ok := r.raw.(driver.RowsColumnTypePrecisionScale); ok {
		return rows.ColumnTypePrecisionScale(index)
	}
	return 0, 0, false
}

func (r *observedRows) ColumnTypeScanType(index int) reflect.Type {
	if rows, ok := r.raw.(driver.RowsColumnTypeScanType); ok {
		return rows.ColumnTypeScanType(index)
	}
	return reflect.TypeOf((*any)(nil)).Elem()
}

func (r *observedRows) finish(err error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.mu.Unlock()
	r.conn.state.statementEndWithMeta(err, r.meta)
}
