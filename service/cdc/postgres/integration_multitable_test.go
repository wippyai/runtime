//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const multiSlot = "wippy_cdc_multi"

func TestMultiTablePublicationRoutesByRelation(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)

	ctx := context.Background()
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS orders (
		id bigserial PRIMARY KEY, sku text NOT NULL, qty int NOT NULL)`)
	require.NoError(t, err)
	var n int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_publication WHERE pubname='multi_pub'`).Scan(&n))
	if n == 0 {
		_, err = db.ExecContext(ctx, `CREATE PUBLICATION multi_pub FOR TABLE accounts, orders`)
		require.NoError(t, err)
	}
	_, err = db.ExecContext(ctx, `DELETE FROM accounts`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM orders`)
	require.NoError(t, err)

	dropNamedSlot(t, repl, multiSlot)
	defer dropNamedSlot(t, repl, multiSlot)

	bus := newCaptureBus()
	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: multiSlot, Publication: "multi_pub",
		Bus: bus, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = src.Start(runCtx)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `INSERT INTO accounts (email, balance) VALUES ('m@w.ai', 1)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO orders (sku, qty) VALUES ('sku-1', 5)`)
	require.NoError(t, err)

	byTable := map[string]RowChange{}
	deadline := time.After(15 * time.Second)
	for len(byTable) < 2 {
		select {
		case e := <-bus.ch:
			if rc, ok := e.Data.(RowChange); ok && rc.Op == OpInsert {
				byTable[rc.Table] = rc
			}
		case <-deadline:
			t.Fatalf("did not receive both inserts, saw: %v", byTable)
		}
	}

	require.Contains(t, byTable, "accounts")
	require.Contains(t, byTable, "orders")
	assert.Equal(t, "m@w.ai", byTable["accounts"].After["email"])
	assert.Equal(t, "sku-1", byTable["orders"].After["sku"])
	assert.Equal(t, "5", byTable["orders"].After["qty"])
	assert.Equal(t, "public", byTable["orders"].Schema)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	stopCancel()
}
