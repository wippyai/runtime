//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
)

func waitForSnapshotEmail(t *testing.T, b *changeCapture, email string, op Op, timeout time.Duration) RowChange {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case rc := <-b.ch:
			if em, _ := rc.After["email"].(string); em == email {
				require.Equal(t, op, rc.Op)
				return rc
			}
		case <-deadline:
			t.Fatalf("no %s change for %q within %s", op, email, timeout)
		}
	}
}

func attachSnapshotCapture(t *testing.T, ctx context.Context, src *Source, capture *changeCapture, capacity ...int) {
	t.Helper()
	size := 8192
	if len(capacity) > 0 && capacity[0] > 0 {
		size = capacity[0]
	}
	stream := src.Subscribe(cdcapi.StreamOptions{Snapshot: true, Buffer: size})
	require.NotNil(t, stream)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, src.Stop(stopCtx))
	})
	t.Cleanup(stream.Close)
	go func() {
		for {
			select {
			case change, ok := <-stream.Changes():
				if !ok {
					return
				}
				capture.send(rowChangeFromAPI(change))
			case <-ctx.Done():
				stream.Close()
				return
			}
		}
	}()
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

	capture := newChangeCapture()
	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
		Snapshot: true, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = src.Start(ctx)
	require.NoError(t, err)
	attachSnapshotCapture(t, ctx, src, capture)

	seen := map[string]Op{}
	deadline := time.After(15 * time.Second)
	for len(seen) < 2 {
		select {
		case rc := <-capture.ch:
			if em, _ := rc.After["email"].(string); em == "snap1@w.ai" || em == "snap2@w.ai" {
				seen[em] = rc.Op
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
		case rc := <-capture.ch:
			if em, _ := rc.After["email"].(string); em == "snap3@w.ai" {
				assert.Equal(t, OpInsert, rc.Op, "post-snapshot change must stream as insert")
				gotSnap3 = true
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

	capture := newChangeCapture()
	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
		Snapshot: true, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = src.Start(ctx)
	require.NoError(t, err)
	attachSnapshotCapture(t, ctx, src, capture)

	rc := waitForSnapshotEmail(t, capture, "null@w.ai", OpSnapshot, 15*time.Second)
	assert.Nil(t, rc.After["note"], "NULL column must map to nil in snapshot row")

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	stopCancel()
}

func TestSnapshotDefaultAppliesPerSubscriberAfterResume(t *testing.T) {
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

	capture := newChangeCapture()
	mk := func(b *changeCapture) *Source {
		return NewSource(SourceOptions{
			ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
			Snapshot: true, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
		})
	}
	src := mk(capture)
	ctx, cancel := context.WithCancel(context.Background())
	_, err = src.Start(ctx)
	require.NoError(t, err)
	attachSnapshotCapture(t, ctx, src, capture)
	waitForSnapshotEmail(t, capture, "resume-base@w.ai", OpSnapshot, 15*time.Second)
	// A per-subscriber snapshot does not advance the source's durable WAL
	// checkpoint. Consume an actual committed transaction before resuming.
	_, err = db.Exec(`UPDATE accounts SET balance=balance+1 WHERE email='resume-base@w.ai'`)
	require.NoError(t, err)
	waitForSnapshotEmail(t, capture, "resume-base@w.ai", OpUpdate, 15*time.Second)
	require.Eventually(t, func() bool {
		var raw string
		e := db.QueryRow(`SELECT lsn FROM wippy_cdc_offsets WHERE slot=$1`, itSlot).Scan(&raw)
		return e == nil && raw != ""
	}, 5*time.Second, 100*time.Millisecond)
	stopCtx, sc := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	sc()
	cancel()

	capture2 := newChangeCapture()
	src2 := mk(capture2)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	_, err = src2.Start(ctx2)
	require.NoError(t, err)
	attachSnapshotCapture(t, ctx2, src2, capture2)

	// Acquisition is asynchronous. Observe the snapshot before requiring the
	// next write to belong to the live side of the handoff.
	waitForSnapshotEmail(t, capture2, "resume-base@w.ai", OpSnapshot, 15*time.Second)
	_, err = db.Exec(`INSERT INTO accounts (email, balance) VALUES ('resume-new@w.ai', 2)`)
	require.NoError(t, err)

	deadline := time.After(15 * time.Second)
	gotNew := false
	for !gotNew {
		select {
		case rc := <-capture2.ch:
			em, _ := rc.After["email"].(string)
			if em == "resume-base@w.ai" {
				assert.Equal(t, OpSnapshot, rc.Op, "entry snapshot is per subscriber, including resumed sources")
			}
			if em == "resume-new@w.ai" {
				assert.Equal(t, OpInsert, rc.Op)
				gotNew = true
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

	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
		StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	_, err = src.Start(ctx)
	require.NoError(t, err)
	stream := src.Subscribe(cdcapi.StreamOptions{Snapshot: true, Buffer: 8})
	require.NotNil(t, stream)
	select {
	case _, ok := <-stream.Changes():
		require.False(t, ok)
	case <-time.After(15 * time.Second):
		t.Fatal("subscriber snapshot did not fail")
	}
	assert.ErrorContains(t, stream.Err(), "injected snapshot failure")
	stream.Close()
	assert.Equal(t, 1, slotCount(t, db, itSlot), "subscriber snapshot failure must not drop the source slot")
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	stopCancel()

	snapshotFailpoint = nil
	capture2 := newChangeCapture()
	src2 := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
		Snapshot: true, StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	_, err = src2.Start(ctx2)
	require.NoError(t, err)
	attachSnapshotCapture(t, ctx2, src2, capture2)
	waitForSnapshotEmail(t, capture2, "retry@w.ai", OpSnapshot, 15*time.Second)

	stopCtx, sc := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src2.Stop(stopCtx))
	sc()
}

func TestPerSubscriberSnapshotHandoffUsesCommitFence(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	const slot = "wippy_cdc_dynamic_snapshot"
	dropNamedSlot(t, repl, slot)
	defer dropNamedSlot(t, repl, slot)

	_, err = db.Exec(`DELETE FROM accounts`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO accounts (email, balance) VALUES ('before@w.ai', 1)`)
	require.NoError(t, err)

	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: slot, Publication: "wippy_cdc_pub",
		StandbyInterval: 200 * time.Millisecond, StatusInterval: time.Hour,
	})
	_, err = src.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = src.Stop(context.Background()) }()

	fenceReady := make(chan struct{})
	releaseSnapshot := make(chan struct{})
	var once sync.Once
	snapshotFailpoint = func() error {
		once.Do(func() { close(fenceReady) })
		<-releaseSnapshot
		return nil
	}
	defer func() { snapshotFailpoint = nil }()

	stream := src.Subscribe(cdcapi.StreamOptions{Snapshot: true, Buffer: 64})
	require.NotNil(t, stream)
	defer stream.Close()
	select {
	case <-fenceReady:
	case <-time.After(15 * time.Second):
		t.Fatal("subscriber snapshot did not establish its exported fence")
	}
	_, err = db.Exec(`INSERT INTO accounts (email, balance) VALUES ('after@w.ai', 2)`)
	require.NoError(t, err)
	close(releaseSnapshot)

	seenBefore := false
	seenAfter := false
	deadline := time.After(15 * time.Second)
	for !seenBefore || !seenAfter {
		select {
		case change, ok := <-stream.Changes():
			require.True(t, ok, "snapshot stream closed: %v", stream.Err())
			email, _ := change.After["email"].(string)
			switch email {
			case "before@w.ai":
				require.Equal(t, OpSnapshot, Op(change.Op))
				seenBefore = true
			case "after@w.ai":
				require.Equal(t, OpInsert, Op(change.Op))
				require.NotEmpty(t, change.CommitLSN)
				seenAfter = true
			}
		case <-deadline:
			t.Fatalf("snapshot/live handoff incomplete: before=%v after=%v", seenBefore, seenAfter)
		}
	}
}
