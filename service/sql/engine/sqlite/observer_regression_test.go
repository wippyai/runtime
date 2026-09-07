// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	config "github.com/wippyai/runtime/api/service/sql"
)

func TestObserverRegressionZeroRowID(t *testing.T) {
	ctx := context.Background()
	o, err := openObservedDB(t, filepath.Join(t.TempDir(), "zero.db"))
	require.NoError(t, err)
	defer o.Close()
	_, err = o.opened.DB.ExecContext(ctx, `CREATE TABLE items(id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	s, err := o.opened.Observer.Subscribe(ctx, config.MutationOptions{})
	require.NoError(t, err)
	defer s.Close()
	_, err = o.opened.DB.ExecContext(ctx, `INSERT INTO items VALUES(0,'valid')`)
	require.NoError(t, err)
	select {
	case b, ok := <-s.Changes():
		require.True(t, ok, "stream closed: %v", s.Err())
		require.Len(t, b.Changes, 1)
	case <-time.After(time.Second):
		t.Fatal("no CDC for committed row zero")
	}
}

func TestObserverRegressionRowIDReuse(t *testing.T) {
	ctx := context.Background()
	o, err := openObservedDB(t, filepath.Join(t.TempDir(), "reuse.db"))
	require.NoError(t, err)
	defer o.Close()
	_, err = o.opened.DB.ExecContext(ctx, `CREATE TABLE items(id INTEGER PRIMARY KEY, value TEXT); INSERT INTO items VALUES(1,'original')`)
	require.NoError(t, err)
	s, err := o.opened.Observer.Subscribe(ctx, config.MutationOptions{})
	require.NoError(t, err)
	defer s.Close()
	tx, err := o.opened.DB.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE items SET id=2 WHERE id=1`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO items VALUES(1,'replacement')`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	b := receiveBatch(t, s)
	require.Len(t, b.Changes, 2, "two final rows must not collapse: %#v", b.Changes)
}

func TestObserverRegressionReleaseSameOffset(t *testing.T) {
	s := &sqliteConnectionState{}
	s.statementSavepointVerb = "savepoint"
	s.statementSavepointName = "outer"
	s.applySavepoint()
	s.statementSavepointName = "inner"
	s.applySavepoint()
	s.statementSavepointVerb = "release"
	s.statementSavepointName = "outer"
	s.applySavepoint()
	require.Empty(t, s.savepoints, "RELEASE outer must remove outer and nested savepoints")
}

func TestObserverRegressionNestedSavepointReuse(t *testing.T) {
	ctx := context.Background()
	o, err := openObservedDB(t, filepath.Join(t.TempDir(), "savepoint-reuse.db"))
	require.NoError(t, err)
	defer o.Close()
	_, err = o.opened.DB.ExecContext(ctx, `CREATE TABLE items(id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	s, err := o.opened.Observer.Subscribe(ctx, config.MutationOptions{})
	require.NoError(t, err)
	defer s.Close()
	tx, err := o.opened.DB.BeginTx(ctx, nil)
	require.NoError(t, err)
	for _, q := range []string{`SAVEPOINT a`, `INSERT INTO items VALUES(1,'rolled back')`, `SAVEPOINT a`, `SAVEPOINT b`, `RELEASE a`, `INSERT INTO items VALUES(2,'rolled back')`, `ROLLBACK TO a`, `RELEASE a`, `INSERT INTO items VALUES(3,'kept')`} {
		_, err = tx.ExecContext(ctx, q)
		require.NoError(t, err, q)
	}
	require.NoError(t, tx.Commit())
	b := receiveBatch(t, s)
	require.Len(t, b.Changes, 1, "must only publish committed row 3: %#v", b.Changes)
	require.Equal(t, int64(3), b.Changes[0].After[0])
}

func TestObserverRegressionSnapshotLiveTextType(t *testing.T) {
	ctx := context.Background()
	o, err := openObservedDB(t, filepath.Join(t.TempDir(), "text.db"))
	require.NoError(t, err)
	defer o.Close()
	_, err = o.opened.DB.ExecContext(ctx, `CREATE TABLE items(id INTEGER PRIMARY KEY, value TEXT); INSERT INTO items VALUES(1,'text')`)
	require.NoError(t, err)
	s, err := o.opened.Observer.Snapshot(ctx, config.SnapshotOptions{Tables: []string{"items"}})
	require.NoError(t, err)
	defer s.Close()
	snap := receiveBatch(t, s)
	_, err = o.opened.DB.ExecContext(ctx, `UPDATE items SET value='text2' WHERE id=1`)
	require.NoError(t, err)
	live := receiveBatch(t, s)
	require.IsType(t, snap.Changes[0].After[1], live.Changes[0].After[1], "same TEXT column changes representation between snapshot and live")
}
