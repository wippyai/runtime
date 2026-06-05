//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func waitForSnapshotEmail(t *testing.T, b *captureBus, email string, op Op, timeout time.Duration) RowChange {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case e := <-b.ch:
			if rc, ok := e.Data.(RowChange); ok {
				if em, _ := rc.After["email"].(string); em == email {
					require.Equal(t, op, rc.Op)
					return rc
				}
			}
		case <-deadline:
			t.Fatalf("no %s change for %q within %s", op, email, timeout)
		}
	}
}

func TestSnapshotBootstrapsExistingRows(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	dropSlot(t, repl)
	defer dropSlot(t, repl)

	_, err = db.Exec(`DELETE FROM accounts`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO accounts (email, balance) VALUES ('snap1@w.ai', 1), ('snap2@w.ai', 2)`)
	require.NoError(t, err)

	bus := newCaptureBus()
	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
		Bus: bus, Snapshot: true, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err = src.Start(ctx)
	require.NoError(t, err)

	seen := map[string]Op{}
	deadline := time.After(15 * time.Second)
	for len(seen) < 2 {
		select {
		case e := <-bus.ch:
			if rc, ok := e.Data.(RowChange); ok {
				if em, _ := rc.After["email"].(string); em == "snap1@w.ai" || em == "snap2@w.ai" {
					seen[em] = rc.Op
				}
			}
		case <-deadline:
			t.Fatalf("snapshot incomplete, saw: %v", seen)
		}
	}
	assert.Equal(t, OpSnapshot, seen["snap1@w.ai"], "pre-existing rows must arrive as snapshot")
	assert.Equal(t, OpSnapshot, seen["snap2@w.ai"])

	_, err = db.Exec(`INSERT INTO accounts (email, balance) VALUES ('snap3@w.ai', 3)`)
	require.NoError(t, err)

	gotSnap3 := false
	deadline2 := time.After(15 * time.Second)
	for !gotSnap3 {
		select {
		case e := <-bus.ch:
			if rc, ok := e.Data.(RowChange); ok {
				if em, _ := rc.After["email"].(string); em == "snap3@w.ai" {
					assert.Equal(t, OpInsert, rc.Op, "post-snapshot change must stream as insert")
					gotSnap3 = true
				}
			}
		case <-deadline2:
			t.Fatal("post-snapshot insert not streamed")
		}
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	stopCancel()
}

func TestSnapshotPreservesNull(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	dropSlot(t, repl)
	defer dropSlot(t, repl)

	_, err = db.Exec(`DELETE FROM accounts`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO accounts (email, balance, note) VALUES ('null@w.ai', 1, NULL)`)
	require.NoError(t, err)

	bus := newCaptureBus()
	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
		Bus: bus, Snapshot: true, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = src.Start(ctx)
	require.NoError(t, err)

	rc := waitForSnapshotEmail(t, bus, "null@w.ai", OpSnapshot, 15*time.Second)
	assert.Nil(t, rc.After["note"], "NULL column must map to nil in snapshot row")

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	stopCancel()
}

func TestSnapshotSkippedOnResume(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	dropSlot(t, repl)
	defer dropSlot(t, repl)

	_, err = db.Exec(`DELETE FROM accounts`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO accounts (email, balance) VALUES ('resume-base@w.ai', 1)`)
	require.NoError(t, err)

	bus := newCaptureBus()
	mk := func(b *captureBus) *Source {
		return NewSource(SourceOptions{
			ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
			Bus: b, Snapshot: true, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
		})
	}
	src := mk(bus)
	ctx, cancel := context.WithCancel(context.Background())
	_, err = src.Start(ctx)
	require.NoError(t, err)
	waitForSnapshotEmail(t, bus, "resume-base@w.ai", OpSnapshot, 15*time.Second)
	require.Eventually(t, func() bool {
		var raw string
		e := db.QueryRow(`SELECT lsn FROM wippy_cdc_offsets WHERE slot=$1`, itSlot).Scan(&raw)
		return e == nil && raw != ""
	}, 5*time.Second, 100*time.Millisecond)
	stopCtx, sc := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	sc()
	cancel()

	bus2 := newCaptureBus()
	src2 := mk(bus2)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	_, err = src2.Start(ctx2)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO accounts (email, balance) VALUES ('resume-new@w.ai', 2)`)
	require.NoError(t, err)

	deadline := time.After(15 * time.Second)
	got := false
	for !got {
		select {
		case e := <-bus2.ch:
			if rc, ok := e.Data.(RowChange); ok {
				assert.NotEqual(t, OpSnapshot, rc.Op, "resume must not re-snapshot existing rows")
				if em, _ := rc.After["email"].(string); em == "resume-new@w.ai" {
					assert.Equal(t, OpInsert, rc.Op)
					got = true
				}
			}
		case <-deadline:
			t.Fatal("resumed source did not stream the new insert")
		}
	}

	stopCtx2, sc2 := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src2.Stop(stopCtx2))
	sc2()
}

func TestSnapshotFailureDropsSlotForCleanRetry(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	dropSlot(t, repl)
	defer dropSlot(t, repl)

	_, err = db.Exec(`DELETE FROM accounts`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO accounts (email, balance) VALUES ('retry@w.ai', 1)`)
	require.NoError(t, err)

	snapshotFailpoint = func() error { return errors.New("injected snapshot failure") }
	defer func() { snapshotFailpoint = nil }()

	bus := newCaptureBus()
	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
		Bus: bus, Snapshot: true, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	status, err := src.Start(ctx)
	require.NoError(t, err)

	select {
	case <-statusClosed(status):
	case <-time.After(15 * time.Second):
		t.Fatal("run did not exit after injected snapshot failure")
	}
	cancel()

	assert.Equal(t, 0, slotCount(t, db, itSlot), "snapshot failure must drop the fresh slot for a clean retry")
	var offsets int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM wippy_cdc_offsets WHERE slot=$1`, itSlot).Scan(&offsets))
	assert.Equal(t, 0, offsets, "snapshot failure must delete the checkpoint")

	snapshotFailpoint = nil
	bus2 := newCaptureBus()
	src2 := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
		Bus: bus2, Snapshot: true, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	_, err = src2.Start(ctx2)
	require.NoError(t, err)
	waitForSnapshotEmail(t, bus2, "retry@w.ai", OpSnapshot, 15*time.Second)

	stopCtx, sc := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src2.Stop(stopCtx))
	sc()
}
