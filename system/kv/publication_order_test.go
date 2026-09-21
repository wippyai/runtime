// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/event"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// synchronousEventBus invokes Send in the caller's goroutine. This makes the
// publication boundary observable: a watch callback must see the snapshot
// that contains the event before Send returns.
type synchronousEventBus struct {
	onSend func(event.Event)
	mu     sync.Mutex
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

func (b *synchronousEventBus) observe(fn func(event.Event)) {
	b.mu.Lock()
	b.onSend = fn
	b.mu.Unlock()
}

func TestServiceWatchEventSeesPublishedSetSnapshot(t *testing.T) {
	bus := new(synchronousEventBus)
	svc := NewService("publication-set", bus, nil)
	if _, err := svc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })

	var callbackErr error
	bus.observe(func(ev event.Event) {
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
	})

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
	bus.observe(func(ev event.Event) {
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
	})

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

func TestServiceWatchDeletionSeesWholePublishedSnapshot(t *testing.T) {
	for _, operation := range []string{"delete", "compare-delete", "transaction", "revoke", "expiry"} {
		t.Run(operation, func(t *testing.T) {
			bus := new(synchronousEventBus)
			svc := NewService("publication-delete", bus, nil)
			if _, err := svc.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = svc.Stop(context.Background()) })
			lease, err := svc.GrantLease(t.Context(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			keys := []string{"first"}
			if operation != "delete" && operation != "compare-delete" {
				keys = append(keys, "second")
			}
			var version kvapi.Version
			for _, key := range keys {
				version, err = svc.SetWithLease(key, []byte(key), lease.ID())
				if err != nil {
					t.Fatal(err)
				}
			}
			seen := 0
			bus.observe(func(ev event.Event) {
				seen++
				watch := ev.Data.(kvapi.WatchEvent)
				if watch.Previous == nil || string(watch.Previous.Value) != watch.Previous.Key {
					t.Errorf("deletion lost its prior value: %+v", watch)
				}
				for _, key := range keys {
					if _, err := svc.Get(key); !errors.Is(err, kvapi.ErrKeyNotFound) {
						t.Errorf("%s remained visible during %s notification: %v", key, operation, err)
					}
				}
			})
			switch operation {
			case "delete":
				err = svc.Delete(keys[0])
			case "compare-delete":
				_, err = svc.CompareAndDelete(keys[0], version)
			case "transaction":
				_, err = svc.Txn([]kvapi.TxnOp{
					{Kind: kvapi.TxnDelete, Key: keys[0]},
					{Kind: kvapi.TxnDelete, Key: keys[1]},
				})
			case "revoke":
				err = lease.Revoke(t.Context())
			case "expiry":
				// Move the lease clock within the serialized owner; no sleep or
				// racing wall-clock timer is needed to exercise expiry.
				err = svc.submitAndWait(func() error {
					svc.leases.renew(lease.ID(), time.Now().Add(-2*lease.TTL()))
					svc.processExpiredLeases()
					return nil
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			if seen != len(keys) {
				t.Fatalf("got %d deletion events, want %d", seen, len(keys))
			}
		})
	}
}

func TestServiceWatchTxnPreservesIntermediateEvents(t *testing.T) {
	bus := new(synchronousEventBus)
	svc := NewService("publication-repeated-key", bus, nil)
	if _, err := svc.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })
	var events []kvapi.WatchEvent
	bus.observe(func(ev event.Event) {
		events = append(events, ev.Data.(kvapi.WatchEvent))
		current, err := svc.Get("key")
		if err != nil || string(current.Value) != "final" {
			t.Errorf("event saw incomplete transaction: current=%+v err=%v", current, err)
		}
	})
	ok, err := svc.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnPut, Key: "key", Value: []byte("first")},
		{Kind: kvapi.TxnDelete, Key: "key"},
		{Kind: kvapi.TxnPut, Key: "key", Value: []byte("final")},
	})
	if err != nil || !ok {
		t.Fatalf("transaction: committed=%v err=%v", ok, err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	if events[0].Current == nil || string(events[0].Current.Value) != "first" ||
		events[1].Type != kvapi.WatchDelete || events[1].Previous == nil || string(events[1].Previous.Value) != "first" ||
		events[2].Current == nil || string(events[2].Current.Value) != "final" || events[2].Previous != nil {
		t.Fatalf("intermediate event values were overwritten: %+v", events)
	}
	ok, err = svc.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: "key", Value: []byte("rejected")},
	})
	if err != nil || ok || len(events) != 3 {
		t.Fatalf("failed transaction emitted a change: committed=%v events=%d err=%v", ok, len(events), err)
	}
}
