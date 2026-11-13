// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/store"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type contextErrorConn struct {
	probeErrorConn
	err         error
	beforeError func(context.Context)
}

func (c *contextErrorConn) QueryContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	if c.beforeError != nil {
		c.beforeError(ctx)
	}
	return nil, c.probeErr
}

func (c *contextErrorConn) ExecContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	if c.beforeError != nil {
		c.beforeError(ctx)
	}
	return nil, c.err
}

func storeOperations() map[string]func(context.Context, *Store) error {
	key := registry.ParseID("test:key")
	value := payload.New("value")
	return map[string]func(context.Context, *Store) error{
		"get":   func(ctx context.Context, s *Store) error { _, err := s.Get(ctx, key); return err },
		"entry": func(ctx context.Context, s *Store) error { _, err := s.Entry(ctx, key); return err },
		"set":   func(ctx context.Context, s *Store) error { return s.Set(ctx, store.Entry{Key: key, Value: value}) },
		"put": func(ctx context.Context, s *Store) error {
			_, err := s.Put(ctx, key, value, store.PutOptions{})
			return err
		},
		"delete": func(ctx context.Context, s *Store) error { return s.Delete(ctx, key) },
		"has":    func(ctx context.Context, s *Store) error { _, err := s.Has(ctx, key); return err },
		"list":   func(ctx context.Context, s *Store) error { _, err := s.List(ctx, store.ListOptions{}); return err },
	}
}

func TestSQLStoreDoneContext(t *testing.T) {
	db := setupTestDB(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	testSQLStoreDoneContext(t, boundarySQLResource{db: db})
}

func testSQLStoreDoneContext(t *testing.T, dbResource resource.Resource[any]) {
	t.Helper()
	for _, deadline := range []bool{false, true} {
		for name, op := range storeOperations() {
			t.Run(fmt.Sprintf("%s/deadline=%t", name, deadline), func(t *testing.T) {
				core, observed := observer.New(zap.ErrorLevel)
				s := NewStore(registry.ParseID("test:store"), createDefaultConfig(), zap.New(core))
				ctx := createTranscoderContext(createContext(boundarySQLRegistry{resource: dbResource}))
				var cancel context.CancelFunc
				if deadline {
					ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				} else {
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				defer cancel()
				err := op(ctx, s)
				require.Equal(t, ctx.Err(), err, "the context error is returned unchanged")
				require.Empty(t, observed.All(), "request cancellation is not a store fault")
			})
		}
	}
}

func TestSQLStoreOperationErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		faults int
	}{
		{"canceled", context.Canceled, 0},
		{"deadline", context.DeadlineExceeded, 0},
		{"wrapped_canceled", fmt.Errorf("query: %w", context.Canceled), 0},
		{"wrapped_deadline", fmt.Errorf("query: %w", context.DeadlineExceeded), 0},
		{"fault", errors.New("database unavailable"), 1},
	} {
		for name, op := range storeOperations() {
			for _, stage := range []string{"query", "acquire", "resource", "write"} {
				if stage == "write" && name != "set" && name != "put" {
					continue
				}
				t.Run(tc.name+"/"+name+"/"+stage, func(t *testing.T) {
					conn := &contextErrorConn{probeErrorConn: probeErrorConn{probeErr: tc.err}, err: tc.err}
					if stage == "write" {
						conn.probeErr = sql.ErrNoRows
					}
					db := sql.OpenDB(boundarySQLConnector{conn: conn})
					t.Cleanup(func() { require.NoError(t, db.Close()) })
					cfg := createDefaultConfig()
					reg := NewMockRegistry()
					reg.resources[cfg.Database] = boundarySQLResource{db: db}
					if stage == "acquire" {
						reg.errors[cfg.Database] = tc.err
					}
					if stage == "resource" {
						reg.resources[cfg.Database] = &MockResource{err: tc.err}
					}
					ctx := createTranscoderContext(createContext(reg))
					core, observed := observer.New(zap.ErrorLevel)
					s := NewStore(registry.ParseID("test:store"), cfg, zap.New(core))
					require.True(t, tc.err == op(ctx, s), "store errors are returned unchanged")
					require.Len(t, observed.All(), tc.faults)
				})
			}
		}
	}
}

func TestSQLStoreCleanupContextErrors(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(err.Error(), func(t *testing.T) {
			conn := &contextErrorConn{err: err}
			db := sql.OpenDB(boundarySQLConnector{conn: conn})
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			core, observed := observer.New(zap.ErrorLevel)
			s := NewStore(registry.ParseID("test:store"), createDefaultConfig(), zap.New(core))
			ctx := createContext(boundarySQLRegistry{resource: boundarySQLResource{db: db}})
			s.cleanup(ctx)
			require.Empty(t, observed.All())
		})
	}
}

func TestSQLStoreCanceledDriverError(t *testing.T) {
	for name, op := range storeOperations() {
		t.Run(name, func(t *testing.T) {
			driverErr := errors.New("statement canceled")
			var cancel context.CancelFunc
			conn := &contextErrorConn{
				probeErrorConn: probeErrorConn{probeErr: driverErr},
				err:            driverErr,
				beforeError:    func(context.Context) { cancel() },
			}
			db := sql.OpenDB(boundarySQLConnector{conn: conn})
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			ctx, cancel := context.WithCancel(createTranscoderContext(createContext(boundarySQLRegistry{resource: boundarySQLResource{db: db}})))
			defer cancel()
			core, observed := observer.New(zap.ErrorLevel)
			s := NewStore(registry.ParseID("test:store"), createDefaultConfig(), zap.New(core))
			err := op(ctx, s)
			require.Same(t, driverErr, err)
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.Empty(t, observed.All(), "driver-specific cancellation errors are not store faults")
		})
	}
}
