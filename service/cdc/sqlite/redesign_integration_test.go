// SPDX-License-Identifier: MPL-2.0

//go:build integration && sqlite_preupdate_hook

package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	config "github.com/wippyai/runtime/api/service/cdc"
)

func TestIntegrationLateSubscriberGetsSnapshotThenLive(t *testing.T) {
	db, _ := openPool(t)
	_, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users (id, email) VALUES (1, 'existing@b.com')`)
	require.NoError(t, err)

	src := newSource(t, db, sourceOptions{})
	_, err = src.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = src.Stop(context.Background()) }()

	stream := src.Subscribe(config.StreamOptions{Snapshot: true})
	defer stream.Close()

	snap := waitChange(t, stream.Changes())
	assert.Equal(t, "snapshot", snap.Op)
	assert.Equal(t, "main", snap.Schema)
	assert.Equal(t, "existing@b.com", snap.After["email"])

	_, err = db.Exec(`INSERT INTO users (id, email) VALUES (2, 'live@b.com')`)
	require.NoError(t, err)

	live := waitChange(t, stream.Changes())
	assert.Equal(t, "insert", live.Op)
	assert.Equal(t, "live@b.com", live.After["email"])
}

func TestIntegrationOpFilteredSubscriberStillGetsSnapshot(t *testing.T) {
	db, _ := openPool(t)
	_, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users (id, email) VALUES (1, 'existing@b.com')`)
	require.NoError(t, err)

	src := newSource(t, db, sourceOptions{})
	_, err = src.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = src.Stop(context.Background()) }()

	stream := src.Subscribe(config.StreamOptions{Snapshot: true, Ops: []string{"insert"}})
	defer stream.Close()

	snap := waitChange(t, stream.Changes())
	assert.Equal(t, "snapshot", snap.Op, "op filter must not drop snapshot rows")
	assert.Equal(t, "existing@b.com", snap.After["email"])
}

func TestIntegrationSecondSourceRefused(t *testing.T) {
	db, _ := openPool(t)
	_, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)
	require.NoError(t, err)

	src1 := newSource(t, db, sourceOptions{name: "src-1"})
	_, err = src1.Start(context.Background())
	require.NoError(t, err)

	src2 := newSource(t, db, sourceOptions{name: "src-2"})
	ctx2, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err = src2.Start(ctx2)
	require.Error(t, err, "a second capture owner on the same database must be refused")

	require.NoError(t, src1.Stop(context.Background()))

	src3 := newSource(t, db, sourceOptions{name: "src-3"})
	_, err = src3.Start(context.Background())
	require.NoError(t, err, "after the first owner stops, a new source may claim capture")
	require.NoError(t, src3.Stop(context.Background()))
}

func TestIntegrationOverflowFaultsWithoutStallingWriter(t *testing.T) {
	db, _ := openPool(t)
	_, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)
	require.NoError(t, err)

	src := newSource(t, db, sourceOptions{})
	src.maxRows = 5
	_, err = src.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = src.Stop(context.Background()) }()

	stream := src.Subscribe(config.StreamOptions{})
	defer stream.Close()

	tx, err := db.Begin()
	require.NoError(t, err)
	for i := 0; i < 20; i++ {
		_, err = tx.Exec(`INSERT INTO t (v) VALUES ('x')`)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit(), "the application commit must succeed even when CDC overflows")

	c := waitChange(t, stream.Changes())
	assert.Equal(t, "error", c.Op)
	assert.NotEmpty(t, c.Error)

	faulted, reason := src.Faulted()
	assert.True(t, faulted)
	assert.NotEmpty(t, reason)

	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM t`).Scan(&n))
	assert.Equal(t, 20, n, "all application rows must be durably written despite the CDC fault")
}

func TestIntegrationSubscribeAfterFaultGetsTerminalError(t *testing.T) {
	db, _ := openPool(t)
	_, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)
	require.NoError(t, err)

	src := newSource(t, db, sourceOptions{})
	src.maxRows = 2
	_, err = src.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = src.Stop(context.Background()) }()

	tx, err := db.Begin()
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		_, err = tx.Exec(`INSERT INTO t (v) VALUES ('x')`)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())

	require.Eventually(t, func() bool {
		faulted, _ := src.Faulted()
		return faulted
	}, 5*time.Second, 10*time.Millisecond)

	late := src.Subscribe(config.StreamOptions{})
	defer late.Close()
	c := waitChange(t, late.Changes())
	assert.Equal(t, "error", c.Op, "a subscriber joining a faulted source must receive a terminal error")
}

func TestIntegrationAlterTableColumnsNotStale(t *testing.T) {
	db, _ := openPool(t)
	_, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, a TEXT)`)
	require.NoError(t, err)

	src := newSource(t, db, sourceOptions{})
	_, err = src.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = src.Stop(context.Background()) }()

	stream := src.Subscribe(config.StreamOptions{})
	defer stream.Close()

	_, err = db.Exec(`INSERT INTO t (id, a) VALUES (1, 'first')`)
	require.NoError(t, err)
	c1 := waitChange(t, stream.Changes())
	assert.Equal(t, "first", c1.After["a"])

	_, err = db.Exec(`ALTER TABLE t ADD COLUMN b TEXT`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO t (id, a, b) VALUES (2, 'second', 'added')`)
	require.NoError(t, err)

	c2 := waitChange(t, stream.Changes())
	assert.Equal(t, "second", c2.After["a"])
	assert.Equal(t, "added", c2.After["b"], "column cache must be invalidated after ALTER TABLE")
}

func TestIntegrationTempTableNotCapturedAsMain(t *testing.T) {
	db, _ := openPool(t)
	_, err := db.Exec(`CREATE TABLE main_t (id INTEGER PRIMARY KEY, v TEXT)`)
	require.NoError(t, err)

	src := newSource(t, db, sourceOptions{})
	_, err = src.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = src.Stop(context.Background()) }()

	stream := src.Subscribe(config.StreamOptions{})
	defer stream.Close()

	_, err = db.Exec(`CREATE TEMP TABLE tmp_t (id INTEGER PRIMARY KEY, v TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO tmp_t (id, v) VALUES (1, 'temp-only')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO main_t (id, v) VALUES (1, 'main-row')`)
	require.NoError(t, err)

	c := waitChange(t, stream.Changes())
	assert.Equal(t, "main", c.Schema)
	assert.Equal(t, "main_t", c.Table)
	assert.Equal(t, "main-row", c.After["v"], "writes to the temp database must not be reported as main")
}

func TestIntegrationStopCleansUpWithExpiredContext(t *testing.T) {
	db, _ := openPool(t)
	_, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)
	require.NoError(t, err)

	src := newSource(t, db, sourceOptions{name: "src-a"})
	_, err = src.Start(context.Background())
	require.NoError(t, err)

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, src.Stop(expired), "Stop must complete cleanup even when the caller context is already cancelled")

	src2 := newSource(t, db, sourceOptions{name: "src-b"})
	_, err = src2.Start(context.Background())
	require.NoError(t, err, "hooks must be released after Stop so a new source can claim capture")
	require.NoError(t, src2.Stop(context.Background()))
}
