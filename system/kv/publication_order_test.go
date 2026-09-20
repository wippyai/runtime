// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/wippyai/runtime/api/event"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// synchronousEventBus invokes Send in the caller's goroutine. This makes the
// publication boundary observable: a watch callback must see the snapshot
// that contains the event before Send returns.
type synchronousEventBus struct {
	mu     sync.Mutex
	onSend func(event.Event)
}

func (b *synchronousEventBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "test", nil
}

func (b *synchronousEventBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "test", nil
}

func (b *synchronousEventBus) Unsubscribe(context.Context, event.SubscriberID) {}

func (b *synchronousEventBus) Send(_ context.Context, ev event.Event) {
	b.mu.Lock()
	onSend := b.onSend
	b.mu.Unlock()
	if onSend != nil {
		onSend(ev)
	}
}

func (*synchronousEventBus) HasSubscribers(event.System, event.Kind) bool { return true }

func TestServiceWatchEventSeesPublishedSetSnapshot(t *testing.T) {
	bus := new(synchronousEventBus)
	svc := NewService("publication-set", bus, nil)
	if _, err := svc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })

	var callbackErr error
	bus.onSend = func(ev event.Event) {
		watch, ok := ev.Data.(kvapi.WatchEvent)
		if !ok || watch.Current == nil {
			callbackErr = fmt.Errorf("unexpected watch payload: %#v", ev.Data)
			return
		}
		current, err := svc.Get("key")
		if err != nil {
			callbackErr = fmt.Errorf("snapshot read during watch callback: %w", err)
			return
		}
		if current.Version < watch.Current.Version || string(current.Value) != "value" {
			callbackErr = fmt.Errorf("callback observed %+v for publication %+v", current, watch.Current)
		}
	}

	if _, err := svc.Set("key", []byte("value")); err != nil {
		t.Fatal(err)
	}
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
}

func TestServiceWatchEventSeesWholePublishedTxnSnapshot(t *testing.T) {
	bus := new(synchronousEventBus)
	svc := NewService("publication-txn", bus, nil)
	if _, err := svc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })

	var callbackErr error
	bus.onSend = func(ev event.Event) {
		watch, ok := ev.Data.(kvapi.WatchEvent)
		if !ok || watch.Current == nil {
			callbackErr = fmt.Errorf("unexpected watch payload: %#v", ev.Data)
			return
		}
		for _, key := range []string{"first", "second"} {
			if _, err := svc.Get(key); err != nil {
				callbackErr = fmt.Errorf("callback saw incomplete transaction for %q after %q: %w", key, watch.Current.Key, err)
				return
			}
		}
	}

	committed, err := svc.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: "first", Value: []byte("1")},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: "second", Value: []byte("2")},
	})
	if err != nil || !committed {
		t.Fatalf("txn: committed=%v err=%v", committed, err)
	}
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
}
