// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
)

// recordingBus captures published events without delivering them.
type recordingBus struct {
	events []event.Event
	mu     sync.Mutex
}

func (b *recordingBus) Subscribe(_ context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (b *recordingBus) SubscribeP(_ context.Context, _ event.System, _ event.Kind, _ chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (b *recordingBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {}

func (b *recordingBus) Send(_ context.Context, e event.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
}

func (b *recordingBus) recorded() []event.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.events
}

func (b *recordingBus) clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = nil
}
