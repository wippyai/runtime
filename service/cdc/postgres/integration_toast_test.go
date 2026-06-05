//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const toastSlot = "wippy_cdc_toast"

func TestUnchangedToastIsMarked(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS toasty (
		id bigint PRIMARY KEY, tag text, big text)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `ALTER TABLE toasty ALTER COLUMN big SET STORAGE EXTERNAL`)
	require.NoError(t, err)
	var n int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_publication WHERE pubname='toasty_pub'`).Scan(&n))
	if n == 0 {
		_, err = db.ExecContext(ctx, `CREATE PUBLICATION toasty_pub FOR TABLE toasty`)
		require.NoError(t, err)
	}
	_, err = db.ExecContext(ctx, `DELETE FROM toasty`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM wippy_cdc_offsets WHERE slot=$1`, toastSlot)
	require.NoError(t, err)

	dropNamedSlot(t, repl, toastSlot)
	defer dropNamedSlot(t, repl, toastSlot)

	bus := newCaptureBus()
	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: toastSlot, Publication: "toasty_pub",
		Bus: bus, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = src.Start(runCtx)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `INSERT INTO toasty (id, tag, big) VALUES (1, 'a', repeat('x', 20000))`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE toasty SET tag = 'b' WHERE id = 1`)
	require.NoError(t, err)

	deadline := time.After(15 * time.Second)
	var upd *RowChange
	for upd == nil {
		select {
		case e := <-bus.ch:
			if rc, ok := e.Data.(RowChange); ok && rc.Op == OpUpdate && rc.Table == "toasty" {
				c := rc
				upd = &c
			}
		case <-deadline:
			t.Fatal("did not receive the toasty update")
		}
	}

	assert.Equal(t, "b", upd.After["tag"], "changed column must carry the new value")
	assert.Equal(t, unchangedTOAST, upd.After["big"],
		"unchanged TOAST column must be marked, not re-sent")

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	stopCancel()
}

func dropNamedSlot(t *testing.T, repl, slot string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgconn.Connect(ctx, repl)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close(ctx) }()
	_ = conn.Exec(ctx, `SELECT pg_drop_replication_slot('`+slot+`')`).Close()
}
