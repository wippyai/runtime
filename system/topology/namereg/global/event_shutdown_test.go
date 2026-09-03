// SPDX-License-Identifier: MPL-2.0

package global

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/event"
	"go.uber.org/zap"
)

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

func TestServiceStopCancelsEventSubscriptionBeforeBarrier(t *testing.T) {
	bus := &cancelBarrierBus{}
	eventCtx, eventCancel := context.WithCancel(context.Background())
	_, _ = bus.Subscribe(eventCtx, "cluster", make(chan event.Event))

	s := &Service{
		bus:         bus,
		logger:      zap.NewNop(),
		stopCh:      make(chan struct{}),
		started:     true,
		eventCancel: eventCancel,
	}
	s.eventWG.Add(1)
	go func() {
		defer s.eventWG.Done()
		s.handleClusterEvents(eventCtx, make(chan event.Event), "sub.cancel-barrier")
	}()

	done := make(chan struct{})
	go func() {
		_ = s.Stop(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop waited at Unsubscribe without canceling its event subscription context")
	}
}
