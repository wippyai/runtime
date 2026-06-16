// SPDX-License-Identifier: MPL-2.0

//go:build integration && sqlite_preupdate_hook

package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	config "github.com/wippyai/runtime/api/service/cdc"
	sqlconfig "github.com/wippyai/runtime/api/service/sql"
	sqlservice "github.com/wippyai/runtime/service/sql"
)

type fakeResource struct{ res sqlservice.DBResource }

func (f *fakeResource) Get() (any, error) { return f.res, nil }
func (f *fakeResource) Release()          {}

type fakeRegistry struct{ db *sql.DB }

func (r *fakeRegistry) Acquire(context.Context, registry.ID, resource.AccessMode) (resource.Resource[any], error) {
	return &fakeResource{res: sqlservice.DBResource{DB: r.db, Type: sqlconfig.SQLite}}, nil
}
func (r *fakeRegistry) List() ([]registry.ID, error) { return nil, nil }
func (r *fakeRegistry) Exists(registry.ID) bool      { return true }

func openPool(t *testing.T) (*sql.DB, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite3_wippy", "file:"+file+"?mode=rwc")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	_, err = db.Exec("PRAGMA journal_mode=WAL")
	require.NoError(t, err)
	return db, file
}

func newSource(t *testing.T, db *sql.DB, opts sourceOptions) *Source {
	t.Helper()
	opts.res = &fakeRegistry{db: db}
	opts.dbResource = registry.NewID("app", "db")
	if opts.name == "" {
		opts.name = "test-src"
	}
	if opts.statusInterval == "" {
		opts.statusInterval = "1s"
	}
	h, err := buildSource(opts)
	require.NoError(t, err)
	return h.(*Source)
}

func waitChange(t *testing.T, ch <-chan config.Change) config.Change {
	t.Helper()
	select {
	case c := <-ch:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for change")
		return config.Change{}
	}
}

func TestIntegrationInsertUpdateDelete(t *testing.T) {
	db, _ := openPool(t)
	_, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT, balance REAL)`)
	require.NoError(t, err)

	src := newSource(t, db, sourceOptions{})
	_, err = src.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = src.Stop(context.Background()) }()

	stream := src.Subscribe(config.StreamOptions{})
	defer stream.Close()

	_, err = db.Exec(`INSERT INTO users (id, email, balance) VALUES (1, 'a@b.com', 42.5)`)
	require.NoError(t, err)
	ins := waitChange(t, stream.Changes())
	assert.Equal(t, "insert", ins.Op)
	assert.Equal(t, "users", ins.Table)
	assert.Equal(t, "a@b.com", ins.After["email"])
	assert.Equal(t, 42.5, ins.After["balance"])
	assert.Equal(t, int64(1), ins.After["id"])
	assert.Nil(t, ins.Before)

	_, err = db.Exec(`UPDATE users SET balance = 99.0 WHERE id = 1`)
	require.NoError(t, err)
	upd := waitChange(t, stream.Changes())
	assert.Equal(t, "update", upd.Op)
	assert.Equal(t, 42.5, upd.Before["balance"])
	assert.Equal(t, 99.0, upd.After["balance"])

	_, err = db.Exec(`DELETE FROM users WHERE id = 1`)
	require.NoError(t, err)
	del := waitChange(t, stream.Changes())
	assert.Equal(t, "delete", del.Op)
	assert.Equal(t, "a@b.com", del.Before["email"])
	assert.Nil(t, del.After)
}

func TestIntegrationValueFidelity(t *testing.T) {
	db, _ := openPool(t)
	_, err := db.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT, qty INTEGER, price REAL, blob BLOB, note TEXT)`)
	require.NoError(t, err)

	src := newSource(t, db, sourceOptions{})
	_, err = src.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = src.Stop(context.Background()) }()

	stream := src.Subscribe(config.StreamOptions{})
	defer stream.Close()

	_, err = db.Exec(`INSERT INTO items (id, name, qty, price, blob, note) VALUES (1, 'widget', 7, 1.25, X'00ff10', NULL)`)
	require.NoError(t, err)

	c := waitChange(t, stream.Changes())
	assert.Equal(t, "widget", c.After["name"])
	assert.Equal(t, int64(7), c.After["qty"])
	assert.Equal(t, 1.25, c.After["price"])
	assert.Equal(t, []byte{0x00, 0xff, 0x10}, c.After["blob"])
	assert.Nil(t, c.After["note"])
}

func TestIntegrationRollbackDiscarded(t *testing.T) {
	db, _ := openPool(t)
	_, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)
	require.NoError(t, err)

	src := newSource(t, db, sourceOptions{})
	_, err = src.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = src.Stop(context.Background()) }()

	stream := src.Subscribe(config.StreamOptions{})
	defer stream.Close()

	tx, err := db.Begin()
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT INTO t (id, v) VALUES (1, 'rolled-back')`)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())

	_, err = db.Exec(`INSERT INTO t (id, v) VALUES (2, 'committed')`)
	require.NoError(t, err)

	c := waitChange(t, stream.Changes())
	assert.Equal(t, "insert", c.Op)
	assert.Equal(t, "committed", c.After["v"])
	assert.Equal(t, int64(2), c.After["id"])
}

func TestIntegrationSnapshotBootstrap(t *testing.T) {
	db, _ := openPool(t)
	_, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users (id, email) VALUES (1, 'existing@b.com')`)
	require.NoError(t, err)

	src := newSource(t, db, sourceOptions{snapshot: true})
	stream := src.Subscribe(config.StreamOptions{})
	defer stream.Close()

	_, err = src.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = src.Stop(context.Background()) }()

	snap := waitChange(t, stream.Changes())
	assert.Equal(t, "snapshot", snap.Op)
	assert.Equal(t, "existing@b.com", snap.After["email"])

	_, err = db.Exec(`INSERT INTO users (id, email) VALUES (2, 'new@b.com')`)
	require.NoError(t, err)
	live := waitChange(t, stream.Changes())
	assert.Equal(t, "insert", live.Op)
	assert.Equal(t, "new@b.com", live.After["email"])
}

func TestIntegrationRestartKeepsCheckpoint(t *testing.T) {
	db, _ := openPool(t)
	_, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users (id, email) VALUES (1, 'existing@b.com')`)
	require.NoError(t, err)

	first := newSource(t, db, sourceOptions{snapshot: true, name: "src"})
	s1 := first.Subscribe(config.StreamOptions{})
	_, err = first.Start(context.Background())
	require.NoError(t, err)
	snap := waitChange(t, s1.Changes())
	require.Equal(t, "snapshot", snap.Op)
	s1.Close()
	require.NoError(t, first.Stop(context.Background()))

	second := newSource(t, db, sourceOptions{snapshot: true, name: "src"})
	s2 := second.Subscribe(config.StreamOptions{})
	defer s2.Close()
	_, err = second.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = second.Stop(context.Background()) }()

	_, err = db.Exec(`INSERT INTO users (id, email) VALUES (2, 'new@b.com')`)
	require.NoError(t, err)

	got := waitChange(t, s2.Changes())
	assert.Equal(t, "insert", got.Op, "restart must not re-snapshot; first event should be the live insert")
	assert.Equal(t, "new@b.com", got.After["email"])
}

func TestIntegrationLaggardDoesNotStallWrites(t *testing.T) {
	db, _ := openPool(t)
	_, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)
	require.NoError(t, err)

	src := newSource(t, db, sourceOptions{})
	_, err = src.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = src.Stop(context.Background()) }()

	laggard := src.Subscribe(config.StreamOptions{Buffer: 1})
	defer laggard.Close()

	done := make(chan error, 1)
	go func() {
		for i := 0; i < 500; i++ {
			if _, e := db.Exec(`INSERT INTO t (v) VALUES ('x')`); e != nil {
				done <- e
				return
			}
		}
		done <- nil
	}()

	select {
	case e := <-done:
		require.NoError(t, e)
	case <-time.After(10 * time.Second):
		t.Fatal("writes stalled: a non-reading subscriber blocked the writer")
	}

	reader := src.Subscribe(config.StreamOptions{})
	defer reader.Close()
	_, err = db.Exec(`INSERT INTO t (v) VALUES ('final')`)
	require.NoError(t, err)

	got := waitChange(t, reader.Changes())
	assert.Equal(t, "insert", got.Op)
	assert.Equal(t, "final", got.After["v"])
}

func TestIntegrationTableAllowlist(t *testing.T) {
	db, _ := openPool(t)
	_, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, v TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, v TEXT)`)
	require.NoError(t, err)

	src := newSource(t, db, sourceOptions{tables: []string{"users"}})
	_, err = src.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = src.Stop(context.Background()) }()

	stream := src.Subscribe(config.StreamOptions{})
	defer stream.Close()

	_, err = db.Exec(`INSERT INTO orders (id, v) VALUES (1, 'ignored')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users (id, v) VALUES (1, 'captured')`)
	require.NoError(t, err)

	c := waitChange(t, stream.Changes())
	assert.Equal(t, "users", c.Table)
	assert.Equal(t, "captured", c.After["v"])
}
