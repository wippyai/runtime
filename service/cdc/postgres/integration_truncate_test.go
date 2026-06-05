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

func TestTruncateIsStreamed(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	dropSlot(t, repl)
	defer dropSlot(t, repl)

	_, err = db.Exec(`DELETE FROM accounts`)
	require.NoError(t, err)

	bus := newCaptureBus()
	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
		Bus: bus, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = src.Start(ctx)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO accounts (email, balance) VALUES ('trunc@w.ai', 1)`)
	require.NoError(t, err)
	waitForEmail(t, bus, "trunc@w.ai", 15*time.Second)

	_, err = db.Exec(`TRUNCATE accounts`)
	require.NoError(t, err)

	deadline := time.After(15 * time.Second)
	var got *RowChange
	for got == nil {
		select {
		case e := <-bus.ch:
			if rc, ok := e.Data.(RowChange); ok && rc.Op == OpTruncate {
				c := rc
				got = &c
			}
		case <-deadline:
			t.Fatal("did not receive a truncate event")
		}
	}
	assert.Equal(t, "accounts", got.Table)
	assert.Equal(t, "public", got.Schema)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	stopCancel()
}
