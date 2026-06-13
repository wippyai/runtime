//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/event"
)

const streamSlot = "wippy_cdc_stream"

func forceStreaming(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`ALTER ROLE cdc_repl SET logical_decoding_work_mem = '64kB'`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Exec(`ALTER ROLE cdc_repl RESET logical_decoding_work_mem`)
	})
}

func TestStreamingLargeTransactionDelivers(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	forceStreaming(t, db)
	_, err = db.Exec(`DELETE FROM accounts`)
	require.NoError(t, err)
	dropNamedSlot(t, repl, streamSlot)
	defer dropNamedSlot(t, repl, streamSlot)

	bus := &captureBus{ch: make(chan event.Event, 8192)}
	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: streamSlot, Publication: "wippy_cdc_pub",
		Bus: bus, Streaming: true, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = src.Start(ctx)
	require.NoError(t, err)

	tx, err := db.Begin()
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT INTO accounts (email, balance) SELECT 'st'||g||'@w.ai', 1 FROM generate_series(1,2000) g`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	seen := map[string]bool{}
	deadline := time.After(30 * time.Second)
	for len(seen) < 2000 {
		select {
		case e := <-bus.ch:
			if rc, ok := e.Data.(RowChange); ok && rc.Op == OpInsert {
				if em, _ := rc.After["email"].(string); strings.HasPrefix(em, "st") {
					seen[em] = true
				}
			}
		case <-deadline:
			t.Fatalf("streamed transaction delivered %d/2000 rows", len(seen))
		}
	}
	assert.Len(t, seen, 2000)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	stopCancel()
}

func TestStreamingAbortedTransactionDiscarded(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	forceStreaming(t, db)
	_, err = db.Exec(`DELETE FROM accounts`)
	require.NoError(t, err)
	dropNamedSlot(t, repl, streamSlot)
	defer dropNamedSlot(t, repl, streamSlot)

	bus := &captureBus{ch: make(chan event.Event, 8192)}
	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: streamSlot, Publication: "wippy_cdc_pub",
		Bus: bus, Streaming: true, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = src.Start(ctx)
	require.NoError(t, err)

	tx, err := db.Begin()
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT INTO accounts (email, balance) SELECT 'ab'||g||'@w.ai', 1 FROM generate_series(1,2000) g`)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())

	_, err = db.Exec(`INSERT INTO accounts (email, balance) VALUES ('marker@w.ai', 1)`)
	require.NoError(t, err)

	gotMarker := false
	deadline := time.After(30 * time.Second)
	for !gotMarker {
		select {
		case e := <-bus.ch:
			if rc, ok := e.Data.(RowChange); ok && rc.Op == OpInsert {
				em, _ := rc.After["email"].(string)
				require.False(t, strings.HasPrefix(em, "ab"), "aborted streamed changes must not be delivered")
				if em == "marker@w.ai" {
					gotMarker = true
				}
			}
		case <-deadline:
			t.Fatal("did not receive the post-rollback marker insert")
		}
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	stopCancel()
}
