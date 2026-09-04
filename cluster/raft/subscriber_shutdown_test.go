// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"go.uber.org/zap"
)

// cancelBarrierBus models a dispatcher stalled on a full subscriber channel:
// Unsubscribe cannot complete until the subscription context is canceled.
// Consumers with their own stop signal must therefore cancel before waiting
// at the ownership barrier.
type cancelBarrierBus struct {
	ctx context.Context
	mu  sync.Mutex
}

func (b *cancelBarrierBus) Subscribe(ctx context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	b.mu.Lock()
	b.ctx = ctx
	b.mu.Unlock()
	return "sub.cancel-barrier", nil
}

func (b *cancelBarrierBus) SubscribeP(ctx context.Context, system event.System, _ event.Kind, ch chan<- event.Event) (event.SubscriberID, error) {
	return b.Subscribe(ctx, system, ch)
}

func (b *cancelBarrierBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {
	b.mu.Lock()
	ctx := b.ctx
	b.mu.Unlock()
	<-ctx.Done()
}

func (*cancelBarrierBus) Send(context.Context, event.Event) {}

func requireStopsAfterCancelingSubscription(t *testing.T, stop func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown waited at Unsubscribe without canceling its subscription context")
	}
}

func TestBootstrapWatcherStopCancelsSubscriptionBeforeBarrier(t *testing.T) {
	bus := &cancelBarrierBus{}
	w := NewBootstrapWatcher(
		"node-a",
		BootstrapWatcherConfig{Expect: 0, Poll: time.Hour},
		newNodeStub(),
		newMemberStub("node-a"),
		bus,
		zap.NewNop(),
	)

	require.NoError(t, w.Start(context.Background()))
	requireStopsAfterCancelingSubscription(t, w.Stop)
}

func TestBootstrapWatcherNaturalExitCancelsSubscriptionBeforeBarrier(t *testing.T) {
	bus := &cancelBarrierBus{}
	node := newNodeStub()
	node.setEstablished("node-b")
	w := NewBootstrapWatcher(
		"node-a",
		BootstrapWatcherConfig{Expect: 0, Poll: time.Millisecond},
		node,
		newMemberStub("node-a"),
		bus,
		zap.NewNop(),
	)

	require.NoError(t, w.Start(context.Background()))
	select {
	case <-w.doneCh:
	case <-time.After(time.Second):
		t.Fatal("natural completion waited at Unsubscribe without canceling its subscription context")
	}
}

func TestMembershipHandlerStopCancelsSubscriptionBeforeBarrier(t *testing.T) {
	bus := &cancelBarrierBus{}
	member := mkNode("node-a", "127.0.0.1")
	h := NewMembershipHandler(
		newFakeRaft(false, nil),
		&fakeMembership{local: member, nodes: []cluster.NodeInfo{member}},
		bus,
		HandlerConfig{ReconcileDebounce: time.Hour},
		zap.NewNop(),
	)

	require.NoError(t, h.Start(context.Background()))
	requireStopsAfterCancelingSubscription(t, h.Stop)
}
