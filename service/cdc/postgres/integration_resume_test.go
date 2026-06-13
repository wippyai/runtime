//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func waitForEmail(t *testing.T, b *captureBus, email string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case e := <-b.ch:
			if rc, ok := e.Data.(RowChange); ok && rc.After["email"] == email {
				return
			}
		case <-deadline:
			t.Fatalf("did not receive change for %q within %s", email, timeout)
		}
	}
}

func TestResumeDeliversChangesMadeWhileDown(t *testing.T) {
	repl, admin := dsns(t)
	adminDB, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = adminDB.Close() }()
	setupSchema(t, adminDB)
	dropSlot(t, repl)
	defer dropSlot(t, repl)

	_, err = adminDB.Exec(`DELETE FROM accounts WHERE email IN ('down0@w.ai','down1@w.ai')`)
	require.NoError(t, err)

	bus := newCaptureBus()
	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
		Bus: bus, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	_, err = src.Start(ctx)
	require.NoError(t, err)

	_, err = adminDB.Exec(`INSERT INTO accounts (email, balance) VALUES ('down0@w.ai', 1)`)
	require.NoError(t, err)
	waitForEmail(t, bus, "down0@w.ai", 15*time.Second)

	require.Eventually(t, func() bool {
		var raw string
		e := adminDB.QueryRow(`SELECT lsn FROM wippy_cdc_offsets WHERE slot=$1`, itSlot).Scan(&raw)
		return e == nil && raw != ""
	}, 5*time.Second, 100*time.Millisecond, "checkpoint must persist before stop")
	time.Sleep(500 * time.Millisecond)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	stopCancel()
	cancel()

	_, err = adminDB.Exec(`INSERT INTO accounts (email, balance) VALUES ('down1@w.ai', 2)`)
	require.NoError(t, err)

	bus2 := newCaptureBus()
	src2 := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
		Bus: bus2, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	_, err = src2.Start(ctx2)
	require.NoError(t, err)

	waitForEmail(t, bus2, "down1@w.ai", 15*time.Second)

	stopCtx2, stopCancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src2.Stop(stopCtx2))
	stopCancel2()
}
