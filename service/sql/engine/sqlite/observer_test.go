// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	config "github.com/wippyai/runtime/api/service/sql"
	sqlservice "github.com/wippyai/runtime/service/sql"
)

func openObservedDB(t *testing.T, file string) (*openedDBForTest, error) {
	t.Helper()
	opened, err := (engine{}).Open(context.Background(), &config.SQLiteConfig{File: file})
	if err != nil {
		return nil, err
	}
	if err := (engine{}).Prepare(context.Background(), opened.DB, &config.SQLiteConfig{File: file}); err != nil {
		_ = opened.DB.Close()
		return nil, err
	}
	(engine{}).Tune(opened.DB, &config.SQLiteConfig{File: file, Pool: config.PoolConfig{MaxLifetime: time.Hour}})
	return &openedDBForTest{opened: opened}, nil
}

type openedDBForTest struct {
	opened sqlservice.OpenedDB
}

func (o *openedDBForTest) Close() {
	if o.opened.Observer != nil {
		_ = o.opened.Observer.Close()
	}
	_ = o.opened.DB.Close()
}

func TestPerPoolObserverCapturesOwnDatabase(t *testing.T) {
	first, err := openObservedDB(t, filepath.Join(t.TempDir(), "first.db"))
	require.NoError(t, err)
	defer first.Close()
	second, err := openObservedDB(t, filepath.Join(t.TempDir(), "second.db"))
	require.NoError(t, err)
	defer second.Close()

	for _, db := range []*openedDBForTest{first, second} {
		_, err := db.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
		require.NoError(t, err)
	}

	firstStream, err := first.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	defer func() { _ = firstStream.Close() }()
	secondStream, err := second.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	defer func() { _ = secondStream.Close() }()

	_, err = first.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (1, 'first')`)
	require.NoError(t, err)
	_, err = second.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (2, 'second')`)
	require.NoError(t, err)

	firstBatch := receiveBatch(t, firstStream)
	secondBatch := receiveBatch(t, secondStream)
	require.Len(t, firstBatch.Changes, 1)
	require.Len(t, secondBatch.Changes, 1)
	assert.Equal(t, []byte("first"), firstBatch.Changes[0].After[1])
	assert.Equal(t, []byte("second"), secondBatch.Changes[0].After[1])

	select {
	case batch := <-firstStream.Changes():
		t.Fatalf("first pool received unrelated batch: %#v", batch)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPerPoolSnapshotLiveIsolation(t *testing.T) {
	first, err := openObservedDB(t, filepath.Join(t.TempDir(), "snapshot-first.db"))
	require.NoError(t, err)
	defer first.Close()
	second, err := openObservedDB(t, filepath.Join(t.TempDir(), "snapshot-second.db"))
	require.NoError(t, err)
	defer second.Close()

	for _, db := range []*openedDBForTest{first, second} {
		_, err = db.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
		require.NoError(t, err)
		_, err = db.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (1, 'initial')`)
		require.NoError(t, err)
	}

	type snapshotResult struct {
		stream config.SnapshotStream
		err    error
	}
	firstSnapshotCh := make(chan snapshotResult, 1)
	secondSnapshotCh := make(chan snapshotResult, 1)
	go func() {
		stream, snapshotErr := first.opened.Observer.Snapshot(context.Background(), config.SnapshotOptions{
			Tables: []string{"items"}, BatchSize: 4096,
		})
		firstSnapshotCh <- snapshotResult{stream: stream, err: snapshotErr}
	}()
	go func() {
		stream, snapshotErr := second.opened.Observer.Snapshot(context.Background(), config.SnapshotOptions{
			Tables: []string{"items"}, BatchSize: 4096,
		})
		secondSnapshotCh <- snapshotResult{stream: stream, err: snapshotErr}
	}()
	firstSnapshot := (<-firstSnapshotCh)
	secondSnapshot := (<-secondSnapshotCh)
	require.NoError(t, firstSnapshot.err)
	require.NoError(t, secondSnapshot.err)
	defer func() { _ = firstSnapshot.stream.Close() }()
	defer func() { _ = secondSnapshot.stream.Close() }()

	firstBatch := receiveBatch(t, firstSnapshot.stream)
	secondBatch := receiveBatch(t, secondSnapshot.stream)
	require.True(t, firstBatch.Snapshot)
	require.True(t, secondBatch.Snapshot)
	assert.Equal(t, "0", firstBatch.Transaction)
	assert.Equal(t, "0", secondBatch.Transaction)

	_, err = first.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (2, 'first-live')`)
	require.NoError(t, err)
	_, err = second.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (2, 'second-live')`)
	require.NoError(t, err)
	firstLive := receiveBatch(t, firstSnapshot.stream)
	secondLive := receiveBatch(t, secondSnapshot.stream)
	require.False(t, firstLive.Snapshot)
	require.False(t, secondLive.Snapshot)
	assert.Equal(t, "1", firstLive.Transaction)
	assert.Equal(t, "1", secondLive.Transaction)
	assert.Equal(t, []byte("first-live"), firstLive.Changes[0].After[1])
	assert.Equal(t, []byte("second-live"), secondLive.Changes[0].After[1])
	require.NoError(t, firstSnapshot.stream.Close())
	require.NoError(t, secondSnapshot.stream.Close())

	firstBackpressured, err := first.opened.Observer.Subscribe(context.Background(), config.MutationOptions{MaxChanges: 1})
	require.NoError(t, err)
	secondLiveStream, err := second.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	_, err = first.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (3, 'first-backpressure')`)
	require.NoError(t, err)
	_, err = second.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (3, 'second-live')`)
	require.NoError(t, err)
	require.Equal(t, int64(3), receiveBatch(t, secondLiveStream).Changes[0].RowID)

	_, err = first.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (4, 'first-overflow')`)
	require.NoError(t, err)
	select {
	case _, ok := <-firstBackpressured.Changes():
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("first pool backpressure stream did not close")
	}
	assert.ErrorIs(t, firstBackpressured.Err(), errObserverOverflow)
	require.NoError(t, first.opened.Observer.Close())

	_, err = second.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (4, 'second-after-close')`)
	require.NoError(t, err)
	secondAfterClose := receiveBatch(t, secondLiveStream)
	assert.Equal(t, int64(4), secondAfterClose.Changes[0].RowID)
	assert.Equal(t, []byte("second-after-close"), secondAfterClose.Changes[0].After[1])
	_ = secondLiveStream.Close()
}

func TestObserverRebindsAfterConnectionExpiry(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "reconnect.db"))
	require.NoError(t, err)
	defer observed.Close()

	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()

	observed.opened.DB.SetConnMaxLifetime(time.Nanosecond)
	time.Sleep(2 * time.Millisecond)
	_, err = observed.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (1, 'rebound')`)
	require.NoError(t, err)

	batch := receiveBatch(t, stream)
	require.Len(t, batch.Changes, 1)
	assert.Equal(t, []byte("rebound"), batch.Changes[0].After[1])
}

func TestObserverClosesWithGeneration(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "close.db"))
	require.NoError(t, err)

	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	require.NoError(t, observed.opened.Observer.Close())

	select {
	case _, ok := <-stream.Changes():
		assert.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("observer stream did not close with its generation")
	}
	assert.ErrorIs(t, stream.Err(), errObserverClosed)
	_ = observed.opened.DB.Close()
}

func TestObserverCloseCancelsSnapshotRead(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "snapshot-close.db"))
	require.NoError(t, err)
	defer observed.opened.DB.Close()
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	for i := 1; i <= 64; i++ {
		_, err = observed.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (?, ?)`, i, "value")
		require.NoError(t, err)
	}
	snapshot, err := observed.opened.Observer.Snapshot(context.Background(), config.SnapshotOptions{
		Tables: []string{"items"}, BatchSize: 1,
	})
	require.NoError(t, err)
	require.NoError(t, observed.opened.Observer.Close())
	select {
	case _, ok := <-snapshot.Changes():
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("snapshot stream did not close after observer shutdown")
	}
	assert.ErrorIs(t, snapshot.Err(), errObserverClosed)
}

func TestObserverPublishesNetTransactionAfterCommit(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "net.db"))
	require.NoError(t, err)
	defer observed.Close()

	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()

	tx, err := observed.opened.DB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (1, 'first')`)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `UPDATE items SET value = 'second' WHERE id = 1`)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `UPDATE items SET value = 'final' WHERE id = 1`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	batch := receiveBatch(t, stream)
	require.Len(t, batch.Changes, 1)
	change := batch.Changes[0]
	assert.Equal(t, "insert", change.Op)
	assert.Nil(t, change.Before)
	assert.Equal(t, []byte("final"), change.After[1])

	tx, err = observed.opened.DB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (2, 'transient')`)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `DELETE FROM items WHERE id = 2`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	select {
	case extra := <-stream.Changes():
		t.Fatalf("insert/delete cycle was published: %#v", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestObserverSavepointRollbackDoesNotPublishRolledBackRows(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "savepoint.db"))
	require.NoError(t, err)
	defer observed.Close()

	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()

	tx, err := observed.opened.DB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (1, 'kept')`)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `SAVEPOINT /* comment */ nested`)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (2, 'rolled-back')`)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `ROLLBACK /* comment */ TO SAVEPOINT nested`)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `RELEASE /* comment */ SAVEPOINT nested`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	batch := receiveBatch(t, stream)
	require.Len(t, batch.Changes, 1)
	assert.Equal(t, int64(1), batch.Changes[0].RowID)
	assert.Equal(t, []byte("kept"), batch.Changes[0].After[1])
	select {
	case extra := <-stream.Changes():
		t.Fatalf("rolled-back row was published: %#v", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestObserverFailsClosedForAmbiguousPartialStatement(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "partial.db"))
	require.NoError(t, err)
	defer observed.Close()

	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT UNIQUE)`)
	require.NoError(t, err)
	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()

	tx, err := observed.opened.DB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `INSERT OR FAIL INTO items (id, value) VALUES (1, 'first'), (2, 'first')`)
	require.Error(t, err)
	require.NoError(t, tx.Commit())

	select {
	case _, ok := <-stream.Changes():
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("observer did not fail closed for ambiguous partial statement")
	}
	assert.ErrorIs(t, stream.Err(), errObserverAmbiguous)
}

func TestObserverSnapshotFencesLiveWrites(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "snapshot.db"))
	require.NoError(t, err)
	defer observed.Close()

	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	for i := 1; i <= 3; i++ {
		_, err = observed.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (?, ?)`, i, "old")
		require.NoError(t, err)
	}

	snapshot, err := observed.opened.Observer.Snapshot(context.Background(), config.SnapshotOptions{
		Tables: []string{"items"}, BatchSize: 1, MaxChanges: 32, MaxBytes: 1 << 20,
	})
	require.NoError(t, err)
	defer func() { _ = snapshot.Close() }()

	writeDone := make(chan error, 1)
	go func() {
		_, writeErr := observed.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (99, 'live')`)
		writeDone <- writeErr
	}()

	var snapshotRows []int64
	for len(snapshotRows) < 3 {
		batch := receiveBatch(t, snapshot)
		require.True(t, batch.Snapshot)
		for _, change := range batch.Changes {
			snapshotRows = append(snapshotRows, change.RowID)
		}
	}
	require.NoError(t, <-writeDone)
	for {
		batch := receiveBatch(t, snapshot)
		if batch.Snapshot {
			continue
		}
		require.Len(t, batch.Changes, 1)
		assert.Equal(t, int64(99), batch.Changes[0].RowID)
		assert.Equal(t, []byte("live"), batch.Changes[0].After[1])
		break
	}
	assert.ElementsMatch(t, []int64{1, 2, 3}, snapshotRows)
}

func TestObserverSnapshotHandoffIncludesInFlightWriterAsLive(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "snapshot-inflight.db"))
	require.NoError(t, err)
	defer observed.Close()
	assert.Equal(t, 0, observed.opened.DB.Stats().MaxOpenConnections, "file-backed SQLite should retain the default unlimited pool")
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	tx, err := observed.opened.DB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (10, 'in-flight')`)
	require.NoError(t, err)

	snapshot, err := observed.opened.Observer.Snapshot(context.Background(), config.SnapshotOptions{Tables: []string{"items"}, BatchSize: 8})
	require.NoError(t, err)
	defer func() { _ = snapshot.Close() }()
	require.NoError(t, tx.Commit())

	select {
	case batch := <-snapshot.Changes():
		if batch.Snapshot {
			t.Fatalf("uncommitted writer appeared in snapshot: %#v", batch)
		}
		require.Len(t, batch.Changes, 1)
		assert.Equal(t, int64(10), batch.Changes[0].RowID)
	case <-time.After(time.Second):
		t.Fatal("in-flight writer did not arrive as live batch")
	}
}

func TestObserverSnapshotFlushesBeforeByteBudget(t *testing.T) {
	value := strings.Repeat("x", 256)
	const maxBytes = 800
	batcher := newSnapshotBatcher("0", 4096, config.DefaultMaxMutationChanges, maxBytes)
	var batches []config.MutationBatch
	emit := func(batch config.MutationBatch) error {
		batches = append(batches, batch)
		return nil
	}
	for i := 1; i <= 8; i++ {
		err := batcher.add(config.Mutation{
			Schema: "main", Table: "items", Columns: []string{"id", "value"},
			RowID: int64(i), After: []any{int64(i), []byte(value)}, Op: "snapshot",
		}, emit)
		require.NoError(t, err)
	}
	require.NoError(t, batcher.flush(emit))
	require.Len(t, batches, 8)
	for _, batch := range batches {
		require.True(t, batch.Snapshot)
		require.NotEmpty(t, batch.Changes)
		assert.LessOrEqual(t, mutationBatchBytes(batch), maxBytes)
		assert.Len(t, batch.Changes, 1)
	}
}

func TestObserverCancelledStreamCannotRemainRegistered(t *testing.T) {
	backend := newSQLiteBackend(config.DefaultMaxMutationChanges, config.DefaultMaxMutationBytes)
	defer func() { _ = backend.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stream := newMutationStream(ctx, backend, config.MutationOptions{
		MaxChanges: config.DefaultMaxMutationChanges,
		MaxBytes:   config.DefaultMaxMutationBytes,
	})
	backend.mu.Lock()
	backend.streams[stream] = struct{}{}
	backend.mu.Unlock()
	stream.start()
	waitForStreamCount(t, backend, 0)
	select {
	case _, ok := <-stream.Changes():
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("cancelled stream did not close")
	}
	assert.ErrorIs(t, stream.Err(), context.Canceled)
}

func TestObserverRemovesCancelledAndOverflowedStreams(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "stream-churn.db"))
	require.NoError(t, err)
	defer observed.Close()
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	backend := observed.opened.Observer.(*sqliteBackend)

	ctx, cancel := context.WithCancel(context.Background())
	cancelled, err := observed.opened.Observer.Subscribe(ctx, config.MutationOptions{})
	require.NoError(t, err)
	cancel()
	waitForStreamCount(t, backend, 0)
	select {
	case _, ok := <-cancelled.Changes():
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("cancelled stream did not close")
	}

	overflowed, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{MaxChanges: 1})
	require.NoError(t, err)
	_, err = observed.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (1, 'one')`)
	require.NoError(t, err)
	_, err = observed.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (2, 'two')`)
	require.NoError(t, err)
	waitForStreamCount(t, backend, 0)
	select {
	case _, ok := <-overflowed.Changes():
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("overflowed stream did not close")
	}
	assert.ErrorIs(t, overflowed.Err(), errObserverOverflow)
}

func TestObserverRelayHandlesManyStreamsWithoutBlockingCommit(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "stream-scale.db"))
	require.NoError(t, err)
	defer observed.Close()
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	backend := observed.opened.Observer.(*sqliteBackend)
	const streamCount = 256
	for range streamCount {
		_, err = observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{MaxChanges: 1})
		require.NoError(t, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = observed.opened.DB.ExecContext(ctx, `INSERT INTO items (id, value) VALUES (1, 'one')`)
	require.NoError(t, err)
	_, err = observed.opened.DB.ExecContext(ctx, `INSERT INTO items (id, value) VALUES (2, 'two')`)
	require.NoError(t, err)
	waitForStreamCount(t, backend, 0)
}

func TestObserverBoundsCommitMarkers(t *testing.T) {
	backend := newSQLiteBackend(2, config.DefaultMaxMutationBytes)
	defer func() { _ = backend.Close() }()
	state := &sqliteConnectionState{backend: backend, maxCommitEnds: 2}

	assert.Equal(t, 0, state.commit())
	assert.Equal(t, 0, state.commit())
	assert.Equal(t, 0, state.commit())
	assert.Len(t, state.commitEnds, 2)
	assert.ErrorIs(t, state.failed, errObserverOverflow)
	state.finalize()
}

func TestObserverAbortedStatementDoesNotPublishPartialRows(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "abort.db"))
	require.NoError(t, err)
	defer observed.Close()
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT UNIQUE)`)
	require.NoError(t, err)
	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()

	tx, err := observed.opened.DB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (1, 'same'), (2, 'same')`)
	require.Error(t, err)
	require.NoError(t, tx.Commit())
	select {
	case _, ok := <-stream.Changes():
		require.False(t, ok, "ambiguous statement must close the observer stream")
	case <-time.After(time.Second):
		t.Fatal("ambiguous statement did not close the observer stream")
	}
	require.ErrorIs(t, stream.Err(), errObserverAmbiguous)
}

func TestObserverFailsClosedWithoutSQLConflictInference(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "conflict-lexing.db"))
	require.NoError(t, err)
	defer observed.Close()
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT UNIQUE)`)
	require.NoError(t, err)
	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()

	tx, err := observed.opened.DB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `INSERT /* OR FAIL */ INTO items (id, value) VALUES (1, 'literal'), (2, 'literal')`)
	require.Error(t, err)
	require.NoError(t, tx.Commit())
	select {
	case _, ok := <-stream.Changes():
		require.False(t, ok)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("observer did not fail closed for ambiguous conflict text")
	}
	assert.ErrorIs(t, stream.Err(), errObserverAmbiguous)
}

func TestObserverFailedSavepointCommandDoesNotCorruptCapture(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "failed-savepoint.db"))
	require.NoError(t, err)
	defer observed.Close()
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()
	tx, err := observed.opened.DB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (1, 'kept')`)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `ROLLBACK TO SAVEPOINT missing`)
	require.Error(t, err)
	require.NoError(t, tx.Commit())
	batch := receiveBatch(t, stream)
	require.Len(t, batch.Changes, 1)
	assert.Equal(t, int64(1), batch.Changes[0].RowID)
}

func TestObserverFailsClosedForMultipleSavepointsInOneExec(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "multi-savepoint.db"))
	require.NoError(t, err)
	defer observed.Close()
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	_, err = observed.opened.DB.ExecContext(context.Background(), `SAVEPOINT first; SAVEPOINT second`)
	require.NoError(t, err)
	select {
	case _, ok := <-stream.Changes():
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("observer did not fail closed for ambiguous multi-savepoint Exec")
	}
	assert.ErrorIs(t, stream.Err(), errObserverAmbiguous)
}

func TestObserverReturningRowsFinalizeOnClose(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "returning.db"))
	require.NoError(t, err)
	defer observed.Close()
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()
	rows, err := observed.opened.DB.QueryContext(context.Background(), `INSERT INTO items (id, value) VALUES (1, 'returned') RETURNING id`)
	require.NoError(t, err)
	var id int64
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&id))
	require.Equal(t, int64(1), id)
	require.NoError(t, rows.Close())
	batch := receiveBatch(t, stream)
	require.Len(t, batch.Changes, 1)
	assert.Equal(t, int64(1), batch.Changes[0].RowID)
}

func TestObserverFiltersLiveMutations(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "filters.db"))
	require.NoError(t, err)
	defer observed.Close()
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT); CREATE TABLE ignored (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{Tables: []string{"items"}, Operations: []string{"insert"}})
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()
	_, err = observed.opened.DB.ExecContext(context.Background(), `INSERT INTO ignored (id, value) VALUES (1, 'no')`)
	require.NoError(t, err)
	_, err = observed.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (2, 'yes')`)
	require.NoError(t, err)
	batch := receiveBatch(t, stream)
	require.Len(t, batch.Changes, 1)
	assert.Equal(t, "items", batch.Changes[0].Table)
	select {
	case extra := <-stream.Changes():
		t.Fatalf("filtered mutation was published: %#v", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestObserverOverflowClosesWithoutBlockingCommit(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "overflow.db"))
	require.NoError(t, err)
	defer observed.Close()
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{MaxChanges: 1, MaxBytes: 1 << 20})
	require.NoError(t, err)
	_, err = observed.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (1, 'one'), (2, 'two')`)
	require.NoError(t, err)
	select {
	case _, ok := <-stream.Changes():
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("overflow stream did not close")
	}
	assert.ErrorIs(t, stream.Err(), errObserverOverflow)
}

func TestObserverKeepsMultiStatementCommitBoundaries(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "boundaries.db"))
	require.NoError(t, err)
	defer observed.Close()
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()
	_, err = observed.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (?,'one'); INSERT INTO items (id, value) VALUES (?,'two')`, 1, 2)
	require.NoError(t, err)
	first := receiveBatch(t, stream)
	second := receiveBatch(t, stream)
	require.Len(t, first.Changes, 1)
	require.Len(t, second.Changes, 1)
	assert.Equal(t, int64(1), first.Changes[0].RowID)
	assert.Equal(t, int64(2), second.Changes[0].RowID)
}

func TestObserverKeepsCommittedPrefixBeforeParameterizedLaterError(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "parameterized-boundary-error.db"))
	require.NoError(t, err)
	defer observed.Close()
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT UNIQUE)`)
	require.NoError(t, err)
	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()
	_, err = observed.opened.DB.ExecContext(context.Background(),
		`INSERT INTO items (id, value) VALUES (?,'one'); INSERT INTO items (id, value) VALUES (?,'one')`,
		1, 2,
	)
	require.Error(t, err)
	select {
	case batch := <-stream.Changes():
		require.Len(t, batch.Changes, 1)
		assert.Equal(t, int64(1), batch.Changes[0].RowID)
	case <-time.After(time.Second):
		t.Fatal("committed prefix was not published")
	}
}

func TestObserverKeepsCommittedPrefixBeforeParameterizedSyntaxTail(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "parameterized-syntax-tail.db"))
	require.NoError(t, err)
	defer observed.Close()
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()
	_, err = observed.opened.DB.ExecContext(context.Background(),
		`INSERT INTO items (id, value) VALUES (?, 'one'); INSER INTO items (id, value) VALUES (?, 'two')`,
		1, 2,
	)
	require.Error(t, err)
	batch := receiveBatch(t, stream)
	require.Len(t, batch.Changes, 1)
	assert.Equal(t, int64(1), batch.Changes[0].RowID)
}

func TestObserverPublishesEarlierAutocommitBeforeLaterError(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "boundary-error.db"))
	require.NoError(t, err)
	defer observed.Close()
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT UNIQUE)`)
	require.NoError(t, err)
	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()
	_, err = observed.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (1,'one')`)
	require.NoError(t, err)
	_, err = observed.opened.DB.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (2,'one')`)
	require.Error(t, err)
	batch := receiveBatch(t, stream)
	require.Len(t, batch.Changes, 1)
	assert.Equal(t, int64(1), batch.Changes[0].RowID)
	select {
	case extra := <-stream.Changes():
		t.Fatalf("failed later statement was published: %#v", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestObserverRejectsUnsupportedVirtualTable(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "virtual.db"))
	require.NoError(t, err)
	defer observed.Close()
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE VIRTUAL TABLE docs USING fts5(content)`)
	if err != nil {
		t.Skipf("sqlite build has no fts5 virtual table: %v", err)
	}
	_, err = observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{Tables: []string{"docs"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "virtual table")
}

func TestObserverFailsClosedWhenVirtualTableIsCreatedDynamically(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "virtual-dynamic.db"))
	require.NoError(t, err)
	defer observed.Close()
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{Tables: []string{"items"}})
	require.NoError(t, err)
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE VIRTUAL TABLE docs USING fts5(content)`)
	if err != nil {
		t.Skipf("sqlite build has no fts5 virtual table: %v", err)
	}
	select {
	case _, ok := <-stream.Changes():
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("observer did not fail closed for dynamic virtual table")
	}
	assert.Contains(t, stream.Err().Error(), "virtual table")
}

func TestObserverDetectsDDLThroughAuthorizerWithLeadingComment(t *testing.T) {
	observed, err := openObservedDB(t, filepath.Join(t.TempDir(), "ddl-comment.db"))
	require.NoError(t, err)
	defer observed.Close()
	_, err = observed.opened.DB.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	stream, err := observed.opened.Observer.Subscribe(context.Background(), config.MutationOptions{})
	require.NoError(t, err)
	tx, err := observed.opened.DB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `/* CREATE TABLE */ CREATE TABLE other (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `INSERT INTO items (id, value) VALUES (1, 'value')`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	select {
	case _, ok := <-stream.Changes():
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("observer did not fail closed for DDL and DML transaction")
	}
	assert.Error(t, stream.Err())
}

func receiveBatch(t *testing.T, stream interface {
	Changes() <-chan config.MutationBatch
}) config.MutationBatch {
	t.Helper()
	select {
	case batch := <-stream.Changes():
		return batch
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mutation batch")
		return config.MutationBatch{}
	}
}

func waitForStreamCount(t *testing.T, backend *sqliteBackend, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		backend.mu.Lock()
		count := len(backend.streams)
		backend.mu.Unlock()
		if count == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	backend.mu.Lock()
	count := len(backend.streams)
	backend.mu.Unlock()
	t.Fatalf("stream count = %d, want %d", count, want)
}
