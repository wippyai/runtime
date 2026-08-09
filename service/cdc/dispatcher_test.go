// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
	cdcsystem "github.com/wippyai/runtime/system/cdc"
)

type dispatcherTestStream struct {
	changes chan cdcapi.Change
	err     error
	closed  atomic.Bool
	once    sync.Once
}

func (s *dispatcherTestStream) Changes() <-chan cdcapi.Change { return s.changes }

func (s *dispatcherTestStream) Close() {
	s.once.Do(func() {
		s.closed.Store(true)
	})
}

func (s *dispatcherTestStream) Err() error { return s.err }

type dispatcherTestSource struct {
	stream *dispatcherTestStream
	info   cdcapi.SourceInfo
}

func (s *dispatcherTestSource) Info() cdcapi.SourceInfo { return s.info }

func (s *dispatcherTestSource) Subscribe(context.Context, cdcapi.StreamOptions) (cdcapi.Stream, error) {
	return s.stream, nil
}

type blockingSubscribeSource struct {
	started chan struct{}
	once    sync.Once
}

func (s *blockingSubscribeSource) Info() cdcapi.SourceInfo {
	return cdcapi.SourceInfo{ID: registry.NewID("test", "blocking")}
}

func (s *blockingSubscribeSource) Subscribe(ctx context.Context, _ cdcapi.StreamOptions) (cdcapi.Stream, error) {
	s.once.Do(func() { close(s.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}

type nilSourceRegistry struct{}

func (nilSourceRegistry) List() []cdcapi.SourceInfo { return nil }

func (nilSourceRegistry) Get(registry.ID) (cdcapi.Source, bool) { return nil, true }

type dispatcherTestNode struct {
	packages chan *relay.Package
	send     func(*relay.Package) error
}

func (n *dispatcherTestNode) ID() pid.NodeID { return "cdc-dispatcher-test" }

func (n *dispatcherTestNode) Send(pkg *relay.Package) error {
	if n.send != nil {
		return n.send(pkg)
	}
	n.packages <- pkg
	return nil
}

func (n *dispatcherTestNode) RegisterHost(pid.HostID, relay.Receiver) error { return nil }
func (n *dispatcherTestNode) UnregisterHost(pid.HostID)                     {}
func (n *dispatcherTestNode) GetHost(pid.HostID) (relay.Receiver, bool)     { return nil, false }
func (n *dispatcherTestNode) Attach(pid.PID, chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}
func (n *dispatcherTestNode) Detach(pid.PID) {}

type dispatcherTestReceiver struct {
	data any
	err  error
	done chan struct{}
	once sync.Once
}

func (r *dispatcherTestReceiver) CompleteYield(_ uint64, data any, err error) {
	r.data = data
	r.err = err
	r.once.Do(func() { close(r.done) })
}

func dispatcherTestContext(t *testing.T, source cdcapi.Source, id registry.ID, node relay.Node) context.Context {
	t.Helper()
	reg := cdcsystem.NewRegistry(nil)
	require.NoError(t, reg.Register(id, source, cdcapi.SQLite))
	return dispatcherTestContextWithRegistry(reg, node)
}

func dispatcherTestContextWithRegistry(reg cdcapi.Registry, node relay.Node) context.Context {
	root := ctxapi.NewRootContext()
	root = cdcapi.WithRegistry(root, reg)
	root = relay.WithNode(root, node)
	ctx, _ := ctxapi.OpenFrameContext(root)
	return ctx
}

func dispatcherTestCommand(id registry.ID) cdcapi.SubscribeCmd {
	target := pid.PID{Host: "test", UniqID: "cdc"}
	return cdcapi.SubscribeCmd{
		PID:    target.Precomputed(),
		Source: id.String(),
		Topic:  "cdc@test",
	}
}

func waitResult(t *testing.T, receiver *dispatcherTestReceiver) {
	t.Helper()
	select {
	case <-receiver.done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for dispatcher result")
	}
}

func TestDispatcherUsesSystemRegistryAndRelaysChanges(t *testing.T) {
	stream := &dispatcherTestStream{changes: make(chan cdcapi.Change, 1)}
	id := registry.NewID("test", "source")
	node := &dispatcherTestNode{packages: make(chan *relay.Package, 2)}
	ctx := dispatcherTestContext(t, &dispatcherTestSource{stream: stream}, id, node)

	d := NewDispatcher(WithWorkers(1))
	require.NoError(t, d.Start(ctx))
	defer func() { require.NoError(t, d.Stop(context.Background())) }()

	receiver := &dispatcherTestReceiver{done: make(chan struct{})}
	require.NoError(t, d.Handle(ctx, dispatcherTestCommand(id), 1, receiver))
	waitResult(t, receiver)
	require.NoError(t, receiver.err)

	sub, ok := receiver.data.(cdcapi.Subscription)
	require.True(t, ok)
	require.NotNil(t, sub.Stop)

	stream.changes <- cdcapi.Change{SourceID: id, Source: id.String(), Op: "insert"}
	select {
	case pkg := <-node.packages:
		require.Len(t, pkg.Messages, 1)
		require.Len(t, pkg.Messages[0].Payloads, 1)
		got, ok := pkg.Messages[0].Payloads[0].Data().(cdcapi.Change)
		require.True(t, ok)
		assert.Equal(t, "insert", got.Op)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for relayed change")
	}

	sub.Stop()
	assert.Eventually(t, stream.closed.Load, time.Second, 10*time.Millisecond)
}

func TestDispatcherRelaysTypedStreamErrorBeforeTerminal(t *testing.T) {
	streamErr := errors.New("capture gap")
	stream := &dispatcherTestStream{changes: make(chan cdcapi.Change), err: streamErr}
	id := registry.NewID("test", "source")
	node := &dispatcherTestNode{packages: make(chan *relay.Package, 2)}
	ctx := dispatcherTestContext(t, &dispatcherTestSource{stream: stream}, id, node)

	d := NewDispatcher(WithWorkers(1))
	require.NoError(t, d.Start(ctx))
	defer func() { require.NoError(t, d.Stop(context.Background())) }()

	receiver := &dispatcherTestReceiver{done: make(chan struct{})}
	require.NoError(t, d.Handle(ctx, dispatcherTestCommand(id), 1, receiver))
	waitResult(t, receiver)
	require.NoError(t, receiver.err)

	close(stream.changes)
	select {
	case pkg := <-node.packages:
		require.Len(t, pkg.Messages, 1)
		payloads := pkg.Messages[0].Payloads
		require.Len(t, payloads, 2)
		gotErr, ok := payloads[0].Data().(error)
		require.True(t, ok)
		assert.ErrorIs(t, gotErr, streamErr)
		assert.True(t, payload.IsTerminal(payloads[1]))
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for typed terminal")
	}
}

func TestDispatcherStopsActiveRelayAndWaitsForCleanup(t *testing.T) {
	stream := &dispatcherTestStream{changes: make(chan cdcapi.Change)}
	id := registry.NewID("test", "source")
	node := &dispatcherTestNode{packages: make(chan *relay.Package, 1)}
	ctx := dispatcherTestContext(t, &dispatcherTestSource{stream: stream}, id, node)

	d := NewDispatcher(WithWorkers(1))
	require.NoError(t, d.Start(ctx))
	receiver := &dispatcherTestReceiver{done: make(chan struct{})}
	require.NoError(t, d.Handle(ctx, dispatcherTestCommand(id), 1, receiver))
	waitResult(t, receiver)
	require.NoError(t, receiver.err)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, d.Stop(stopCtx))
	assert.True(t, stream.closed.Load())

	postStop := &dispatcherTestReceiver{done: make(chan struct{})}
	require.NoError(t, d.Handle(ctx, dispatcherTestCommand(id), 2, postStop))
	waitResult(t, postStop)
	assert.ErrorIs(t, postStop.err, ErrDispatcherNotStarted)
}

func TestDispatcherCancelsRelayAfterNodeFailure(t *testing.T) {
	relayErr := errors.New("node unavailable")
	var sends atomic.Int32
	node := &dispatcherTestNode{send: func(*relay.Package) error {
		sends.Add(1)
		return relayErr
	}}
	stream := &dispatcherTestStream{changes: make(chan cdcapi.Change, 1)}
	id := registry.NewID("test", "source")
	ctx := dispatcherTestContext(t, &dispatcherTestSource{stream: stream}, id, node)

	d := NewDispatcher(WithWorkers(1))
	require.NoError(t, d.Start(ctx))
	defer func() { require.NoError(t, d.Stop(context.Background())) }()
	receiver := &dispatcherTestReceiver{done: make(chan struct{})}
	require.NoError(t, d.Handle(ctx, dispatcherTestCommand(id), 1, receiver))
	waitResult(t, receiver)
	require.NoError(t, receiver.err)

	stream.changes <- cdcapi.Change{Source: id.String(), Op: "insert"}
	assert.Eventually(t, stream.closed.Load, time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(1), sends.Load())
}

func TestDispatcherRejectsNilRegistrySource(t *testing.T) {
	id := registry.NewID("test", "nil")
	node := &dispatcherTestNode{packages: make(chan *relay.Package, 1)}
	ctx := dispatcherTestContextWithRegistry(nilSourceRegistry{}, node)

	d := NewDispatcher(WithWorkers(1))
	require.NoError(t, d.Start(ctx))
	defer func() { require.NoError(t, d.Stop(context.Background())) }()

	receiver := &dispatcherTestReceiver{done: make(chan struct{})}
	require.NoError(t, d.Handle(ctx, dispatcherTestCommand(id), 1, receiver))
	waitResult(t, receiver)
	assert.ErrorIs(t, receiver.err, ErrNilSource)
}

func TestDispatcherStopCancelsBlockingSubscribe(t *testing.T) {
	source := &blockingSubscribeSource{started: make(chan struct{})}
	id := registry.NewID("test", "blocking")
	reg := cdcsystem.NewRegistry(nil)
	require.NoError(t, reg.Register(id, source, cdcapi.SQLite))
	node := &dispatcherTestNode{packages: make(chan *relay.Package, 1)}
	ctx := dispatcherTestContextWithRegistry(reg, node)

	d := NewDispatcher(WithWorkers(1))
	require.NoError(t, d.Start(ctx))
	receiver := &dispatcherTestReceiver{done: make(chan struct{})}
	require.NoError(t, d.Handle(ctx, dispatcherTestCommand(id), 1, receiver))
	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for blocking subscription")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, d.Stop(stopCtx))
	waitResult(t, receiver)
	assert.ErrorIs(t, receiver.err, ErrDispatcherStopping)
}

func TestDispatcherHandleAndStopAreSafeConcurrently(t *testing.T) {
	root := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(root)
	d := NewDispatcher(WithWorkers(2))
	require.NoError(t, d.Start(ctx))
	require.ErrorIs(t, d.Start(ctx), ErrDispatcherStarted)

	const commands = 32
	receivers := make([]*dispatcherTestReceiver, commands)
	var wg sync.WaitGroup
	for i := 0; i < commands; i++ {
		receiver := &dispatcherTestReceiver{done: make(chan struct{})}
		receivers[i] = receiver
		wg.Add(1)
		go func(tag uint64, receiver *dispatcherTestReceiver) {
			defer wg.Done()
			_ = d.Handle(ctx, dispatcherTestCommand(registry.NewID("test", "source")), tag, receiver)
		}(uint64(i), receiver)
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- d.Stop(context.Background()) }()
	wg.Wait()
	require.NoError(t, <-stopDone)
	for _, receiver := range receivers {
		waitResult(t, receiver)
	}
}
