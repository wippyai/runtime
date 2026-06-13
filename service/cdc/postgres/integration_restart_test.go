//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRestartSameInstanceAfterFailure(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	dropSlot(t, repl)
	defer dropSlot(t, repl)

	_, err = db.Exec(`DELETE FROM accounts WHERE email IN ('rs1@w.ai','rs2@w.ai')`)
	require.NoError(t, err)

	bus := newCaptureBus()
	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
		Bus: bus, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	status, err := src.Start(ctx)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO accounts (email, balance) VALUES ('rs1@w.ai', 1)`)
	require.NoError(t, err)
	waitForEmail(t, bus, "rs1@w.ai", 15*time.Second)

	_, err = db.Exec(`SELECT pg_terminate_backend(active_pid) FROM pg_replication_slots
		WHERE slot_name = $1 AND active_pid IS NOT NULL`, itSlot)
	require.NoError(t, err)

	select {
	case <-statusClosed(status):
	case <-time.After(15 * time.Second):
		t.Fatal("run goroutine did not exit after backend termination")
	}

	_, err = src.Start(ctx)
	require.NoError(t, err, "Start must succeed on the same instance after a failure (supervisor retry contract)")

	_, err = db.Exec(`INSERT INTO accounts (email, balance) VALUES ('rs2@w.ai', 2)`)
	require.NoError(t, err)
	waitForEmail(t, bus, "rs2@w.ai", 15*time.Second)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	stopCancel()
}

func statusClosed(status <-chan any) <-chan struct{} {
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for range status {
		}
	}()
	return closed
}
