// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	sqlconfig "github.com/wippyai/runtime/api/service/sql"
	sqlstore "github.com/wippyai/runtime/api/service/store/sql"
	"github.com/wippyai/runtime/api/store"
	sqlsvc "github.com/wippyai/runtime/service/sql"
	"go.uber.org/zap"
)

type boundarySQLConnector struct{ conn driver.Conn }

func (c boundarySQLConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c boundarySQLConnector) Driver() driver.Driver                        { return boundarySQLDriver{} }

type boundarySQLDriver struct{}

func (boundarySQLDriver) Open(string) (driver.Conn, error) { return nil, errors.New("unused") }

type probeErrorConn struct {
	probeErr  error
	execCount atomic.Int32
}

func (c *probeErrorConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (c *probeErrorConn) Close() error                        { return nil }
func (c *probeErrorConn) Begin() (driver.Tx, error)           { return nil, errors.New("unused") }
func (c *probeErrorConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, c.probeErr
}
func (c *probeErrorConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.execCount.Add(1)
	return driver.RowsAffected(1), nil
}

type boundarySQLResource struct{ db *sql.DB }

func (r boundarySQLResource) Get() (any, error) {
	return sqlsvc.DBResource{DB: r.db, Type: sqlconfig.SQLite}, nil
}
func (boundarySQLResource) Release() {}

type boundarySQLRegistry struct{ resource resource.Resource[any] }

func (r boundarySQLRegistry) Acquire(context.Context, registry.ID, resource.AccessMode) (resource.Resource[any], error) {
	return r.resource, nil
}
func (boundarySQLRegistry) List() ([]registry.ID, error) { return nil, nil }
func (boundarySQLRegistry) Exists(registry.ID) bool      { return true }

type boundarySQLTranscoder struct{}

func (boundarySQLTranscoder) Unmarshal(payload.Payload, any) error { return errors.New("unused") }
func (boundarySQLTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return payload.NewPayload([]byte(p.Data().(string)), payload.JSON), nil
}

func TestD01SQLStoreProbeErrorPreventsWrite(t *testing.T) {
	probeErr := errors.New("probe failed")
	conn := &probeErrorConn{probeErr: probeErr}
	db := sql.OpenDB(boundarySQLConnector{conn: conn})
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	cfg := &sqlstore.Config{
		Database:          registry.ParseID("test:db"),
		TableName:         "entries",
		IDColumnName:      "id",
		PayloadColumnName: "payload",
		ExpireColumnName:  "expires_at",
	}
	ctx := resource.WithRegistry(ctxapi.NewRootContext(), boundarySQLRegistry{resource: boundarySQLResource{db: db}})
	ctx = payload.WithTranscoder(ctx, boundarySQLTranscoder{})
	s := NewStore(registry.ParseID("test:store"), cfg, zap.NewNop())

	err := s.Set(ctx, store.Entry{Key: registry.ParseID("test:key"), Value: payload.New("value")})
	require.ErrorIs(t, err, probeErr)
	require.Zero(t, conn.execCount.Load(), "an indeterminate existence probe must not write")
}
