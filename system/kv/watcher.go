// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"strings"

	"github.com/wippyai/runtime/api/event"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// watcher implements kvapi.Watcher using the event bus.
type watcher struct {
	ctx    context.Context
	bus    event.Bus
	events chan kvapi.WatchEvent
	cancel context.CancelFunc
	subID  event.SubscriberID
}

// newWatcher creates a watcher that subscribes to the event bus for a KV
// instance. If prefix is empty, it watches all keys (subscribes to the
// full system). If prefix is set, it subscribes to "system" and filters
// by key prefix in the delivery goroutine.
func newWatcher(ctx context.Context, bus event.Bus, system event.System, prefix string) (*watcher, error) {
	ctx, cancel := context.WithCancel(ctx)

	ch := make(chan event.Event, 64)
	watchCh := make(chan kvapi.WatchEvent, 64)

	// Subscribe to all events for this KV system.
	// Prefix filtering happens in the delivery goroutine because
	// event bus wildcard matching uses glob patterns, not prefix matching.
	subID, err := bus.Subscribe(ctx, system, ch)
	if err != nil {
		cancel()
		return nil, err
	}

	w := &watcher{
		events: watchCh,
		cancel: cancel,
		subID:  subID,
		bus:    bus,
		ctx:    ctx,
	}

	go w.deliver(ch, prefix)
	return w, nil
}

// deliver reads from the event bus channel, filters by prefix, and forwards
// to the watcher's output channel.
func (w *watcher) deliver(source <-chan event.Event, prefix string) {
	defer close(w.events)
	for {
		select {
		case evt, ok := <-source:
			if !ok {
				return
			}
			watchEvt, ok := evt.Data.(kvapi.WatchEvent)
			if !ok {
				continue
			}

			// Prefix filter: event Kind is the key
			if prefix != "" {
				if !strings.HasPrefix(evt.Kind, prefix) {
					continue
				}
			}

			select {
			case w.events <- watchEvt:
			case <-w.ctx.Done():
				return
			}

		case <-w.ctx.Done():
			return
		}
	}
}

func (w *watcher) Events() <-chan kvapi.WatchEvent {
	return w.events
}

func (w *watcher) Close() error {
	w.cancel()
	w.bus.Unsubscribe(context.Background(), w.subID)
	return nil
}

var _ kvapi.Watcher = (*watcher)(nil)
