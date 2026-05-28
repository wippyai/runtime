// SPDX-License-Identifier: MPL-2.0

package eventbus

import (
	"context"
	"errors"
	"testing"

	"github.com/wippyai/runtime/api/event"
)

// TestSubscriberCapRejection verifies that SubscribeP returns
// ErrSubscribersCapReached once the bus's maxSubscribers cap is hit,
// preventing unbounded growth from a runaway leak.
func TestSubscriberCapRejection(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	const cap = 8
	bus.maxSubscribers = cap

	ctx := context.Background()

	for i := 0; i < cap; i++ {
		ch := make(chan event.Event, 1)
		if _, err := bus.Subscribe(ctx, "x", ch); err != nil {
			t.Fatalf("subscribe %d: unexpected error: %v", i, err)
		}
	}

	overflowCh := make(chan event.Event, 1)
	_, err := bus.Subscribe(ctx, "x", overflowCh)
	if !errors.Is(err, ErrSubscribersCapReached) {
		t.Fatalf("expected ErrSubscribersCapReached, got %v", err)
	}
}

// TestSubscriberCapRecoversAfterUnsubscribe ensures the cap is
// LIVE-counted: Unsubscribe frees a slot.
func TestSubscriberCapRecoversAfterUnsubscribe(t *testing.T) {
	bus := NewBus()
	defer bus.Stop()

	const cap = 4
	bus.maxSubscribers = cap

	ctx := context.Background()
	ids := make([]event.SubscriberID, 0, cap)
	for i := 0; i < cap; i++ {
		ch := make(chan event.Event, 1)
		id, err := bus.Subscribe(ctx, "x", ch)
		if err != nil {
			t.Fatalf("subscribe %d: %v", i, err)
		}
		ids = append(ids, id)
	}

	// Cap reached.
	if _, err := bus.Subscribe(ctx, "x", make(chan event.Event, 1)); !errors.Is(err, ErrSubscribersCapReached) {
		t.Fatalf("expected cap rejection, got %v", err)
	}

	// Free one slot.
	bus.Unsubscribe(ctx, ids[0])

	// Now another subscribe should succeed.
	if _, err := bus.Subscribe(ctx, "x", make(chan event.Event, 1)); err != nil {
		t.Fatalf("post-unsubscribe Subscribe failed: %v", err)
	}
}
