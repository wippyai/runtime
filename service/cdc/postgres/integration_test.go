//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/event"

	_ "github.com/lib/pq"
)

const itSlot = "wippy_cdc_it"

type captureBus struct {
	ch chan event.Event
}

func newCaptureBus() *captureBus { return &captureBus{ch: make(chan event.Event, 256)} }

func (b *captureBus) Send(_ context.Context, e event.Event) {
	select {
	case b.ch <- e:
	default:
	}
}

func (b *captureBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (b *captureBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (b *captureBus) Unsubscribe(context.Context, event.SubscriberID) {}

func dsns(t *testing.T) (repl, admin string) {
	t.Helper()
	repl = os.Getenv("WIPPY_CDC_IT_REPL_DSN")
	admin = os.Getenv("WIPPY_CDC_IT_ADMIN_DSN")
	if repl == "" || admin == "" {
		t.Skip("set WIPPY_CDC_IT_REPL_DSN and WIPPY_CDC_IT_ADMIN_DSN to run cdc integration test")
	}
	return repl, admin
}

func setupSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS accounts (
		id bigserial PRIMARY KEY, email text NOT NULL, balance numeric(12,2) NOT NULL DEFAULT 0,
		note text, updated_at timestamptz NOT NULL DEFAULT now())`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `ALTER TABLE accounts REPLICA IDENTITY FULL`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DROP TABLE IF EXISTS wippy_cdc_offsets`)
	require.NoError(t, err)
	var n int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_publication WHERE pubname='wippy_cdc_pub'`).Scan(&n))
	if n == 0 {
		_, err = db.ExecContext(ctx, `CREATE PUBLICATION wippy_cdc_pub FOR TABLE accounts`)
		require.NoError(t, err)
	}
}

func dropSlot(t *testing.T, repl string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgconn.Connect(ctx, repl)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close(ctx) }()
	_ = conn.Exec(ctx, `SELECT pg_drop_replication_slot('`+itSlot+`')`).Close()
}

func collectOps(t *testing.T, b *captureBus, timeout time.Duration) []RowChange {
	t.Helper()
	deadline := time.After(timeout)
	var got []RowChange
	seen := map[Op]bool{}
	for {
		select {
		case e := <-b.ch:
			if rc, ok := e.Data.(RowChange); ok {
				got = append(got, rc)
				seen[rc.Op] = true
				if seen[OpInsert] && seen[OpUpdate] && seen[OpDelete] {
					return got
				}
			}
		case <-deadline:
			return got
		}
	}
}

func TestSourceStreamsAndResumes(t *testing.T) {
	repl, admin := dsns(t)
	adminDB, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = adminDB.Close() }()
	setupSchema(t, adminDB)
	dropSlot(t, repl)
	defer dropSlot(t, repl)

	_, err = adminDB.Exec(`DELETE FROM accounts WHERE email IN ('it@w.ai','it2@w.ai')`)
	require.NoError(t, err)

	bus := newCaptureBus()
	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
		Bus: bus, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	_, err = src.Start(ctx)
	require.NoError(t, err)

	_, err = adminDB.Exec(`INSERT INTO accounts (email, balance) VALUES ('it@w.ai', 10)`)
	require.NoError(t, err)
	_, err = adminDB.Exec(`UPDATE accounts SET balance = 20 WHERE email='it@w.ai'`)
	require.NoError(t, err)
	_, err = adminDB.Exec(`DELETE FROM accounts WHERE email='it@w.ai'`)
	require.NoError(t, err)

	got := collectOps(t, bus, 15*time.Second)
	ops := map[Op]int{}
	for _, c := range got {
		ops[c.Op]++
		assert.Equal(t, "accounts", c.Table)
	}
	assert.GreaterOrEqual(t, ops[OpInsert], 1)
	assert.GreaterOrEqual(t, ops[OpUpdate], 1)
	assert.GreaterOrEqual(t, ops[OpDelete], 1)
	assert.Equal(t, "it@w.ai", got[0].After["email"])

	require.Eventually(t, func() bool {
		var raw string
		err := adminDB.QueryRow(`SELECT lsn FROM wippy_cdc_offsets WHERE slot=$1`, itSlot).Scan(&raw)
		return err == nil && raw != ""
	}, 5*time.Second, 100*time.Millisecond, "checkpoint LSN must be durably persisted")

	var cp1 string
	require.NoError(t, adminDB.QueryRow(`SELECT lsn FROM wippy_cdc_offsets WHERE slot=$1`, itSlot).Scan(&cp1))
	require.NotEmpty(t, cp1)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	stopCancel()
	cancel()

	bus2 := newCaptureBus()
	src2 := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
		Bus: bus2, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	_, err = src2.Start(ctx2)
	require.NoError(t, err)

	_, err = adminDB.Exec(`INSERT INTO accounts (email, balance) VALUES ('it2@w.ai', 99)`)
	require.NoError(t, err)

	deadline := time.After(15 * time.Second)
	gotNew := false
	for !gotNew {
		select {
		case e := <-bus2.ch:
			if rc, ok := e.Data.(RowChange); ok && rc.After["email"] == "it2@w.ai" {
				gotNew = true
			}
		case <-deadline:
			t.Fatal("did not receive post-restart insert within timeout")
		}
	}
	assert.True(t, gotNew, "resumed source must stream new changes")

	var cp2 string
	require.NoError(t, adminDB.QueryRow(`SELECT lsn FROM wippy_cdc_offsets WHERE slot=$1`, itSlot).Scan(&cp2))
	var advanced bool
	require.NoError(t, adminDB.QueryRow(
		`SELECT $1::pg_lsn >= $2::pg_lsn`, cp2, cp1).Scan(&advanced))
	assert.True(t, advanced, "checkpoint must advance monotonically across restart (resume, not re-snapshot)")

	stopCtx2, stopCancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src2.Stop(stopCtx2))
	stopCancel2()
}
