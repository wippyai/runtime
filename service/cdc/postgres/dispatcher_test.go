// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
)

type fakeCDCStream struct {
	changes chan cdcapi.Change
	closed  atomic.Bool
}

func (s *fakeCDCStream) Changes() <-chan cdcapi.Change {
	return s.changes
}

func (s *fakeCDCStream) Close() {
	s.closed.Store(true)
}

type fakeCDCStreamer struct {
	stream *fakeCDCStream
	source string
	opts   cdcapi.StreamOptions
}

func (s *fakeCDCStreamer) Stream(_ context.Context, source string, opts cdcapi.StreamOptions) (cdcapi.ChangeStream, cdcapi.SourceInfo, error) {
	s.source = source
	s.opts = opts
	return s.stream, cdcapi.SourceInfo{Name: "test:cdc", Slot: source}, nil
}

type captureCDCNode struct {
	packages chan *relay.Package
}

func (n *captureCDCNode) ID() pid.NodeID { return "cdc-test-node" }

func (n *captureCDCNode) Send(pkg *relay.Package) error {
	n.packages <- pkg
	return nil
}

func (n *captureCDCNode) RegisterHost(pid.HostID, relay.Receiver) error { return nil }
func (n *captureCDCNode) UnregisterHost(pid.HostID)                     {}
func (n *captureCDCNode) GetHost(pid.HostID) (relay.Receiver, bool)     { return nil, false }
func (n *captureCDCNode) Attach(pid.PID, chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}
func (n *captureCDCNode) Detach(pid.PID) {}

type cdcResultReceiver struct {
	done chan struct{}
	data any
	err  error
}

func (r *cdcResultReceiver) CompleteYield(_ uint64, data any, err error) {
	r.data = data
	r.err = err
	close(r.done)
}

func TestDispatcherSubscribeRelaysChangesAndCleanupStopsStream(t *testing.T) {
	root := ctxapi.NewRootContext()
	node := &captureCDCNode{packages: make(chan *relay.Package, 2)}
	root = relay.WithNode(root, node)
	ctx, _ := ctxapi.OpenFrameContext(root)

	stream := &fakeCDCStream{changes: make(chan cdcapi.Change, 1)}
	streamer := &fakeCDCStreamer{stream: stream}
	ctx = cdcapi.WithSourceStreamer(ctx, streamer)

	d := NewDispatcher(WithWorkers(1))
	require.NoError(t, d.Start(ctx))
	defer func() { require.NoError(t, d.Stop(ctx)) }()

	target := pid.PID{Host: "cdc-test", UniqID: "p1"}
	target = target.Precomputed()
	receiver := &cdcResultReceiver{done: make(chan struct{})}
	err := d.handle(ctx, cdcapi.SubscribeCmd{
		Source: "slot_a",
		Topic:  "cdc@1",
		PID:    target,
		Options: cdcapi.StreamOptions{
			Ops:    []string{"insert"},
			Buffer: 2,
		},
	}, 1, receiver)
	require.NoError(t, err)

	select {
	case <-receiver.done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscribe result")
	}
	require.NoError(t, receiver.err)
	sub, ok := receiver.data.(cdcapi.Subscription)
	require.True(t, ok)
	assert.Equal(t, "slot_a", streamer.source)
	assert.Equal(t, []string{"insert"}, streamer.opts.Ops)

	stream.changes <- cdcapi.Change{Source: "test:cdc", Op: "insert", Relation: "public.accounts"}
	select {
	case pkg := <-node.packages:
		require.Equal(t, target, pkg.Target)
		require.Len(t, pkg.Messages, 1)
		require.Equal(t, "cdc@1", pkg.Messages[0].Topic)
		require.Len(t, pkg.Messages[0].Payloads, 1)
		got, ok := pkg.Messages[0].Payloads[0].Data().(cdcapi.Change)
		require.True(t, ok)
		assert.Equal(t, "public.accounts", got.Relation)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for relayed cdc change")
	}

	sub.Stop()
	require.Eventually(t, stream.closed.Load, time.Second, 10*time.Millisecond)
}

func TestDispatcherSubscribeRelaysTerminalOnSourceClose(t *testing.T) {
	root := ctxapi.NewRootContext()
	node := &captureCDCNode{packages: make(chan *relay.Package, 2)}
	root = relay.WithNode(root, node)
	ctx, _ := ctxapi.OpenFrameContext(root)

	stream := &fakeCDCStream{changes: make(chan cdcapi.Change)}
	ctx = cdcapi.WithSourceStreamer(ctx, &fakeCDCStreamer{stream: stream})

	d := NewDispatcher(WithWorkers(1))
	require.NoError(t, d.Start(ctx))
	defer func() { require.NoError(t, d.Stop(ctx)) }()

	receiver := &cdcResultReceiver{done: make(chan struct{})}
	target := pid.PID{Host: "cdc-test", UniqID: "p1"}
	target = target.Precomputed()
	require.NoError(t, d.handle(ctx, cdcapi.SubscribeCmd{
		Source: "slot_a",
		Topic:  "cdc@1",
		PID:    target,
	}, 1, receiver))
	select {
	case <-receiver.done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscribe result")
	}
	require.NoError(t, receiver.err)

	close(stream.changes)
	select {
	case pkg := <-node.packages:
		require.Len(t, pkg.Messages, 1)
		require.Len(t, pkg.Messages[0].Payloads, 1)
		assert.True(t, payload.IsTerminal(pkg.Messages[0].Payloads[0]))
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for terminal package")
	}
}
