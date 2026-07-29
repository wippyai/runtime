// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

type terminalRowsConnector struct{ terminalErr error }

func (c terminalRowsConnector) Connect(context.Context) (driver.Conn, error) {
	return &terminalRowsConn{terminalErr: c.terminalErr}, nil
}
func (terminalRowsConnector) Driver() driver.Driver { return terminalRowsDriver{} }

type terminalRowsDriver struct{}

func (terminalRowsDriver) Open(string) (driver.Conn, error) { return nil, errors.New("unused") }

type terminalRowsConn struct{ terminalErr error }

func (*terminalRowsConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (*terminalRowsConn) Close() error                        { return nil }
func (*terminalRowsConn) Begin() (driver.Tx, error)           { return nil, errors.New("unused") }
func (c *terminalRowsConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &terminalRows{terminalErr: c.terminalErr}, nil
}

type terminalRows struct {
	terminalErr error
	yielded     bool
}

func (*terminalRows) Columns() []string { return []string{"value"} }
func (*terminalRows) Close() error      { return nil }
func (r *terminalRows) Next(dest []driver.Value) error {
	if !r.yielded {
		r.yielded = true
		dest[0] = "partial"
		return nil
	}
	if r.terminalErr != nil {
		err := r.terminalErr
		r.terminalErr = nil
		return err
	}
	return io.EOF
}

func TestD16RowsTerminalErrorWins(t *testing.T) {
	terminalErr := errors.New("terminal rows failure")
	db := sql.OpenDB(terminalRowsConnector{terminalErr: terminalErr})
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	rows, err := db.QueryContext(t.Context(), "boundary")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rows.Close()) })

	response := scanRows(rows)
	require.ErrorIs(t, response.Error, terminalErr)
	require.Nil(t, response.Columns)
	require.Nil(t, response.Rows, "partial rows must not be reported as success")
}
