// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
	sqlservice "github.com/wippyai/runtime/service/sql"
)

type testResourceRegistry struct {
	observer sqlapi.CommittedMutationSource
	releases atomic.Int32
}

func (r *testResourceRegistry) Acquire(context.Context, registry.ID, resource.AccessMode) (resource.Resource[any], error) {
	return &testDBResource{owner: r, value: sqlservice.DBResource{
		Type:     sqlapi.SQLite,
		Observer: r.observer,
	}}, nil
}

func (*testResourceRegistry) List() ([]registry.ID, error) { return nil, nil }
func (*testResourceRegistry) Exists(registry.ID) bool      { return true }

type testDBResource struct {
	owner *testResourceRegistry
	value sqlservice.DBResource
	once  sync.Once
}

func (r *testDBResource) Get() (any, error) { return r.value, nil }
func (r *testDBResource) Release() {
	r.once.Do(func() { r.owner.releases.Add(1) })
}

type testObserver struct {
	stream              *testMutationStream
	snapshot            *testSnapshotStream
	snapshotStarted     chan struct{}
	subOpts             sqlapi.MutationOptions
	snapshotCancelDelay time.Duration
	mu                  sync.Mutex
	closeN              atomic.Int32
	closed              bool
}

func (o *testObserver) Subscribe(ctx context.Context, opts sqlapi.MutationOptions) (sqlapi.MutationStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil, errors.New("test observer closed")
	}
	o.subOpts = opts
	o.stream = newTestMutationStream()
	return o.stream, nil
}

func (o *testObserver) Snapshot(ctx context.Context, _ sqlapi.SnapshotOptions) (sqlapi.SnapshotStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if o.snapshotStarted != nil {
		select {
		case o.snapshotStarted <- struct{}{}:
		default:
		}
		<-ctx.Done()
		if o.snapshotCancelDelay > 0 {
			time.Sleep(o.snapshotCancelDelay)
		}
		return nil, ctx.Err()
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil, errors.New("test observer closed")
	}
	o.snapshot = &testSnapshotStream{
		testMutationStream: newTestMutationStream(),
		watermark:          "watermark-1",
	}
	return o.snapshot, nil
}

func (o *testObserver) Close() error {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return nil
	}
	o.closed = true
	stream := o.stream
	snapshot := o.snapshot
	o.mu.Unlock()
	o.closeN.Add(1)
	if stream != nil {
		stream.closeWithError(errors.New("test SQL generation closed"))
	}
	if snapshot != nil {
		snapshot.closeWithError(errors.New("test SQL generation closed"))
	}
	return nil
}

func (o *testObserver) currentStream(t *testing.T) *testMutationStream {
	t.Helper()
	o.mu.Lock()
	stream := o.stream
	o.mu.Unlock()
	require.NotNil(t, stream)
	return stream
}

func (o *testObserver) currentSnapshot(t *testing.T) *testSnapshotStream {
	t.Helper()
	o.mu.Lock()
	stream := o.snapshot
	o.mu.Unlock()
	require.NotNil(t, stream)
	return stream
}

type testMutationStream struct {
	err     error
	changes chan sqlapi.MutationBatch
	mu      sync.Mutex
	closeN  atomic.Int32
	closed  bool
}

type testSnapshotStream struct {
	*testMutationStream
	watermark string
}

func (s *testSnapshotStream) Watermark() string { return s.watermark }

func newTestMutationStream() *testMutationStream {
	return &testMutationStream{changes: make(chan sqlapi.MutationBatch, 16)}
}

func (s *testMutationStream) Changes() <-chan sqlapi.MutationBatch { return s.changes }

func (s *testMutationStream) Err() error {
	s.mu.Lock()
	err := s.err
	s.mu.Unlock()
	return err
}

func (s *testMutationStream) Close() error {
	s.closeWithError(nil)
	return nil
}

func (s *testMutationStream) closeWithError(err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.err = err
	close(s.changes)
	s.mu.Unlock()
	s.closeN.Add(1)
}

func (s *testMutationStream) push(batch sqlapi.MutationBatch) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.changes <- batch
	}
}

func newTestSource(t *testing.T, observer *testObserver, opts sourceOptions) *Source {
	t.Helper()
	resources := &testResourceRegistry{observer: observer}
	if opts.res == nil {
		opts.res = resources
	}
	if opts.id.Name == "" {
		opts.id = registry.NewID("app", "cdc")
	}
	if opts.name == "" {
		opts.name = opts.id.String()
	}
	source, err := buildSource(opts)
	require.NoError(t, err)
	return source.(*Source)
}

func receiveChange(t *testing.T, stream cdcapi.Stream) cdcapi.Change {
	t.Helper()
	select {
	case change, ok := <-stream.Changes():
		if !ok {
			require.Failf(t, "snapshot/live stream closed", "stream error: %v", stream.Err())
		}
		return change
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SQLite CDC change")
		return cdcapi.Change{}
	}
}

func requireNoChangeSource(t *testing.T, stream cdcapi.Stream) {
	t.Helper()
	select {
	case change, ok := <-stream.Changes():
		if !ok {
			t.Fatalf("stream closed while expecting no change")
		}
		t.Fatalf("unexpected change: %#v", change)
	case <-time.After(50 * time.Millisecond):
	}
}

func waitStreamClosed(t *testing.T, stream cdcapi.Stream) error {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-stream.Changes():
			if !ok {
				return stream.Err()
			}
		case <-deadline:
			t.Fatal("timed out waiting for SQLite CDC stream close")
			return nil
		}
	}
}

func TestSourceForwardsCommittedMutationWithStableShape(t *testing.T) {
	observer := &testObserver{}
	source := newTestSource(t, observer, sourceOptions{tables: []string{"users"}})
	status, err := source.Start(context.Background())
	require.NoError(t, err)
	require.Equal(t, "sqlite cdc source started", <-status)
	defer func() { require.NoError(t, source.Stop(context.Background())) }()

	stream, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{Ops: []string{"update"}})
	require.NoError(t, err)
	defer stream.Close()

	assert.Equal(t, []string{"users"}, observer.subOpts.Tables)
	observer.currentStream(t).push(sqlapi.MutationBatch{
		Transaction: "17",
		Changes: []sqlapi.Mutation{{
			Schema:  "main",
			Table:   "users",
			Columns: []string{"id", "name"},
			Before:  []any{int64(1), []byte("old")},
			After:   []any{int64(1), []byte("new")},
			Op:      "update",
		}},
	})

	change := receiveChange(t, stream)
	assert.Equal(t, source.name, change.Source)
	assert.Equal(t, source.id, change.SourceID)
	assert.Equal(t, "update", change.Op)
	assert.Equal(t, "main", change.Schema)
	assert.Equal(t, "users", change.Relation)
	assert.Equal(t, "17/0", change.Cursor)
	assert.Equal(t, "17", change.Transaction)
	assert.Equal(t, int64(1), change.After["id"])
	assert.Equal(t, []byte("old"), change.Before["name"])
	assert.Equal(t, []byte("new"), change.After["name"])

	info := source.Info()
	assert.True(t, info.Capabilities.Snapshot, "a tagged SQL observer exposes the snapshot handoff capability")
	assert.False(t, info.Capabilities.Durable)
	assert.False(t, info.Capabilities.Replayable)
	assert.False(t, info.Capabilities.CapturesExternalWrites)
	assert.True(t, info.Capabilities.BeforeImages)
}

func TestSourceRejectsResumeButSupportsSnapshotHandoff(t *testing.T) {
	observer := &testObserver{}
	source := newTestSource(t, observer, sourceOptions{})
	_, err := source.Start(context.Background())
	require.NoError(t, err)
	defer func() { require.NoError(t, source.Stop(context.Background())) }()

	_, err = source.Subscribe(context.Background(), cdcapi.StreamOptions{After: "17/0"})
	assert.ErrorIs(t, err, cdcapi.ErrUnsupported)
	snapshot, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{Snapshot: true})
	require.NoError(t, err)
	snapshot.Close()
}

func TestSourceSnapshotSubscriberOwnsAtomicHandoff(t *testing.T) {
	observer := &testObserver{}
	source := newTestSource(t, observer, sourceOptions{})
	_, err := source.Start(context.Background())
	require.NoError(t, err)
	defer func() { require.NoError(t, source.Stop(context.Background())) }()

	live, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{})
	require.NoError(t, err)
	defer live.Close()
	snapshot, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{
		Snapshot: true,
	})
	require.NoError(t, err)
	defer snapshot.Close()

	observer.currentSnapshot(t).push(sqlapi.MutationBatch{
		Transaction: "snapshot-1",
		Snapshot:    true,
		Changes: []sqlapi.Mutation{{
			Schema: "main", Table: "users", Columns: []string{"id", "name"},
			After: []any{int64(1), "existing"}, Op: "insert",
		}},
	})
	observer.currentSnapshot(t).push(sqlapi.MutationBatch{
		Transaction: "live-1",
		Changes: []sqlapi.Mutation{{
			Schema: "main", Table: "users", Columns: []string{"id", "name"},
			After: []any{int64(2), "new"}, Op: "insert",
		}},
	})

	snapshotChange := receiveChange(t, snapshot)
	assert.Equal(t, "snapshot", snapshotChange.Op)
	assert.Equal(t, "watermark-1/snapshot-1/0", snapshotChange.Cursor)
	assert.Equal(t, "existing", snapshotChange.After["name"])
	liveChange := receiveChange(t, snapshot)
	assert.Equal(t, "insert", liveChange.Op)
	assert.Equal(t, "new", liveChange.After["name"])
	requireNoChangeSource(t, live)
}

func TestSourceDefaultSnapshotAppliesPerSubscriber(t *testing.T) {
	observer := &testObserver{}
	source := newTestSource(t, observer, sourceOptions{snapshot: true})
	_, err := source.Start(context.Background())
	require.NoError(t, err)
	defer func() { require.NoError(t, source.Stop(context.Background())) }()

	stream, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{})
	require.NoError(t, err)
	observer.currentSnapshot(t).push(sqlapi.MutationBatch{
		Transaction: "snapshot-default",
		Snapshot:    true,
		Changes: []sqlapi.Mutation{{
			Schema: "main", Table: "users", Columns: []string{"id"}, After: []any{int64(7)}, Op: "insert",
		}},
	})
	assert.Equal(t, "snapshot", receiveChange(t, stream).Op)
}

func TestSourceSnapshotClosesUpstreamWhenItEnds(t *testing.T) {
	observer := &testObserver{}
	source := newTestSource(t, observer, sourceOptions{})
	_, err := source.Start(context.Background())
	require.NoError(t, err)
	defer func() { require.NoError(t, source.Stop(context.Background())) }()

	stream, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{Snapshot: true})
	require.NoError(t, err)
	upstream := observer.currentSnapshot(t)
	expected := errors.New("snapshot worker failed")
	upstream.closeWithError(expected)

	assert.ErrorIs(t, waitStreamClosed(t, stream), expected)
	assert.Eventually(t, func() bool { return upstream.closeN.Load() == 1 }, time.Second, time.Millisecond)
}

func TestSourceSnapshotOverflowClosesUpstream(t *testing.T) {
	observer := &testObserver{}
	source := newTestSource(t, observer, sourceOptions{})
	_, err := source.Start(context.Background())
	require.NoError(t, err)
	defer func() { require.NoError(t, source.Stop(context.Background())) }()

	stream, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{
		Snapshot: true,
		Buffer:   1,
	})
	require.NoError(t, err)
	upstream := observer.currentSnapshot(t)
	change := func(id int64) sqlapi.MutationBatch {
		return sqlapi.MutationBatch{
			Transaction: "snapshot",
			Snapshot:    true,
			Changes: []sqlapi.Mutation{{
				Schema: "main", Table: "users", Columns: []string{"id"},
				After: []any{id}, Op: "insert",
			}},
		}
	}
	upstream.push(change(1))
	subscriber := stream.(*subscription)
	assert.Eventually(t, func() bool {
		subscriber.mu.Lock()
		defer subscriber.mu.Unlock()
		return len(subscriber.changes) == 1
	}, time.Second, time.Millisecond)
	upstream.push(change(2))

	assert.Eventually(t, func() bool { return upstream.closeN.Load() == 1 }, time.Second, time.Millisecond)
	assert.ErrorIs(t, subscriber.Err(), errSubscriberOverflow)
	stream.Close()
}

func TestSourceStopWaitsForBlockedSnapshotAcquisition(t *testing.T) {
	observer := &testObserver{
		snapshotStarted:     make(chan struct{}, 1),
		snapshotCancelDelay: 50 * time.Millisecond,
	}
	source := newTestSource(t, observer, sourceOptions{})
	_, err := source.Start(context.Background())
	require.NoError(t, err)

	subscribeDone := make(chan error, 1)
	go func() {
		_, subscribeErr := source.Subscribe(context.Background(), cdcapi.StreamOptions{Snapshot: true})
		subscribeDone <- subscribeErr
	}()
	select {
	case <-observer.snapshotStarted:
	case <-time.After(time.Second):
		t.Fatal("snapshot acquisition did not start")
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- source.Stop(context.Background()) }()
	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned before the blocked snapshot acquisition ended: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	require.NoError(t, <-stopDone)
	assert.Error(t, <-subscribeDone)

	source.mu.RLock()
	assert.Empty(t, source.snapshotAcq)
	assert.Empty(t, source.snapshotSubs)
	assert.Equal(t, cdcapi.SourceStateStopped, source.state)
	source.mu.RUnlock()
}

func TestSourceStopsWithoutClosingSQLGeneration(t *testing.T) {
	observer := &testObserver{}
	resources := &testResourceRegistry{observer: observer}
	source := newTestSource(t, observer, sourceOptions{res: resources})
	_, err := source.Start(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(1), resources.releases.Load(), "the ordinary resource borrow ends after Subscribe")

	stream, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{})
	require.NoError(t, err)
	snapshot, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{Snapshot: true})
	require.NoError(t, err)
	require.NoError(t, source.Stop(context.Background()))
	assert.Equal(t, int32(0), observer.closeN.Load(), "CDC does not own the SQL observer")
	assert.NoError(t, waitStreamClosed(t, stream))
	assert.NoError(t, waitStreamClosed(t, snapshot))
}

func TestSourceCanRestartAfterStop(t *testing.T) {
	observer := &testObserver{}
	resources := &testResourceRegistry{observer: observer}
	source := newTestSource(t, observer, sourceOptions{res: resources})

	_, err := source.Start(context.Background())
	require.NoError(t, err)
	first, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{})
	require.NoError(t, err)
	require.NoError(t, source.Stop(context.Background()))
	assert.Equal(t, cdcapi.SourceStateStopped, source.Info().State)
	assert.NoError(t, waitStreamClosed(t, first))

	_, err = source.Start(context.Background())
	require.NoError(t, err, "a supervisor stop is restartable; only resource-generation disposal is terminal")
	second, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{})
	require.NoError(t, err)
	observer.currentStream(t).push(sqlapi.MutationBatch{
		Transaction: "2",
		Changes: []sqlapi.Mutation{{
			Schema: "main", Table: "t", Columns: []string{"id"}, After: []any{int64(2)}, Op: "insert",
		}},
	})
	assert.Equal(t, int64(2), receiveChange(t, second).After["id"])
	require.NoError(t, source.Stop(context.Background()))
	assert.Equal(t, int32(2), resources.releases.Load())
}

func TestSourceFaultsWhenSQLGenerationCloses(t *testing.T) {
	observer := &testObserver{}
	source := newTestSource(t, observer, sourceOptions{})
	_, err := source.Start(context.Background())
	require.NoError(t, err)
	stream, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{})
	require.NoError(t, err)

	require.NoError(t, observer.Close())
	assert.Error(t, waitStreamClosed(t, stream))
	assert.Equal(t, cdcapi.SourceStateFaulted, source.Info().State)
	assert.NotEmpty(t, source.Info().Error)
	assert.NoError(t, source.Stop(context.Background()))
}

func TestSourceContextCancellationClosesSubscribers(t *testing.T) {
	observer := &testObserver{}
	source := newTestSource(t, observer, sourceOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	_, err := source.Start(ctx)
	require.NoError(t, err)
	stream, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{})
	require.NoError(t, err)

	cancel()
	assert.ErrorIs(t, waitStreamClosed(t, stream), context.Canceled)
	assert.Equal(t, cdcapi.SourceStateFaulted, source.Info().State)
	require.NoError(t, source.Stop(context.Background()))
}

func TestSourceCanRestartAgainstReplacementSQLGeneration(t *testing.T) {
	firstObserver := &testObserver{}
	resources := &testResourceRegistry{observer: firstObserver}
	source := newTestSource(t, firstObserver, sourceOptions{res: resources})
	_, err := source.Start(context.Background())
	require.NoError(t, err)
	first, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{})
	require.NoError(t, err)
	require.NoError(t, firstObserver.Close())
	assert.Error(t, waitStreamClosed(t, first))
	require.NoError(t, source.Stop(context.Background()))

	secondObserver := &testObserver{}
	resources.observer = secondObserver
	_, err = source.Start(context.Background())
	require.NoError(t, err)
	second, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{})
	require.NoError(t, err)
	secondObserver.currentStream(t).push(sqlapi.MutationBatch{
		Transaction: "replacement-1",
		Changes: []sqlapi.Mutation{{
			Schema: "main", Table: "t", Columns: []string{"id"}, After: []any{int64(9)}, Op: "insert",
		}},
	})
	assert.Equal(t, int64(9), receiveChange(t, second).After["id"])
	require.NoError(t, source.Stop(context.Background()))
}

func TestSourceClosesOnlyOverflowedSubscriber(t *testing.T) {
	observer := &testObserver{}
	source := newTestSource(t, observer, sourceOptions{})
	_, err := source.Start(context.Background())
	require.NoError(t, err)
	defer func() { require.NoError(t, source.Stop(context.Background())) }()

	laggard, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{Buffer: 1})
	require.NoError(t, err)
	reader, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{Buffer: 8})
	require.NoError(t, err)

	batch := sqlapi.MutationBatch{Transaction: "1", Changes: []sqlapi.Mutation{
		{Schema: "main", Table: "t", Columns: []string{"id"}, After: []any{int64(1)}, Op: "insert"},
		{Schema: "main", Table: "t", Columns: []string{"id"}, After: []any{int64(2)}, Op: "insert"},
	}}
	observer.currentStream(t).push(batch)

	assert.Equal(t, "insert", receiveChange(t, reader).Op)
	assert.ErrorIs(t, waitStreamClosed(t, laggard), errSubscriberOverflow)
	assert.Equal(t, "insert", receiveChange(t, reader).Op)
}

func TestSourceMalformedMutationFaultsClosedStream(t *testing.T) {
	observer := &testObserver{}
	source := newTestSource(t, observer, sourceOptions{})
	_, err := source.Start(context.Background())
	require.NoError(t, err)
	stream, err := source.Subscribe(context.Background(), cdcapi.StreamOptions{})
	require.NoError(t, err)

	observer.currentStream(t).push(sqlapi.MutationBatch{Transaction: "1", Changes: []sqlapi.Mutation{{
		Table: "t", Columns: nil, After: []any{int64(1)}, Op: "insert",
	}}})
	assert.Error(t, waitStreamClosed(t, stream))
	assert.Equal(t, cdcapi.SourceStateFaulted, source.Info().State)
	assert.NoError(t, source.Stop(context.Background()))
}
