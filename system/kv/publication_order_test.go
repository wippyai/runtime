// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"errors"
	"testing"
	"time"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func watchPublished(t *testing.T, svc *Service) kvapi.Watcher {
	t.Helper()
	w, err := svc.Watch(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func nextPublished(t *testing.T, w kvapi.Watcher) kvapi.WatchEvent {
	t.Helper()
	select {
	case <-w.Done():
		t.Fatalf("watch invalid before notification: %v", w.Err())
	case ev, ok := <-w.Events():
		if !ok {
			t.Fatal("watch closed before notification")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("watch notification missing")
	}
	return kvapi.WatchEvent{}
}

func startPublishedService(t *testing.T) *Service {
	t.Helper()
	svc := NewService("publication", nil)
	if _, err := svc.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })
	return svc
}

func TestServiceWatchEventSeesPublishedSetSnapshot(t *testing.T) {
	svc := startPublishedService(t)
	w := watchPublished(t, svc)
	if _, err := svc.Set("key", []byte("value")); err != nil {
		t.Fatal(err)
	}
	ev := nextPublished(t, w)
	if ev.Current == nil || string(ev.Current.Value) != "value" {
		t.Fatalf("unexpected watch event: %+v", ev)
	}
	current, err := svc.Get("key")
	if err != nil || current.Version < ev.Current.Version || string(current.Value) != "value" {
		t.Fatalf("watch observed unpublished set: current=%+v ev=%+v err=%v", current, ev, err)
	}
}

func TestServiceWatchEventSeesWholePublishedTxnSnapshot(t *testing.T) {
	svc := startPublishedService(t)
	w := watchPublished(t, svc)
	committed, err := svc.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: "first", Value: []byte("1")},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: "second", Value: []byte("2")},
	})
	if err != nil || !committed {
		t.Fatalf("txn: committed=%v err=%v", committed, err)
	}
	for range 2 {
		ev := nextPublished(t, w)
		for _, key := range []string{"first", "second"} {
			if _, err := svc.Get(key); err != nil {
				t.Fatalf("incomplete transaction after %q: %v", ev.Current.Key, err)
			}
		}
	}
}

// Observe a notification while the event-loop action is still occupied. A
// response-time read would miss a regression that queued watch events before
// storing the complete snapshot and only repaired the snapshot before return.
func TestServiceWatchNotificationDuringUnfinishedAction(t *testing.T) {
	svc := startPublishedService(t)
	w := watchPublished(t, svc)
	published := make(chan struct{})
	release := make(chan struct{})
	actionDone := make(chan struct{})
	t.Cleanup(func() { close(release) })
	svc.submit(func() {
		svc.state.set("first", []byte("1"), "")
		svc.emitPut("first", nil)
		svc.state.set("second", []byte("2"), "")
		svc.emitPut("second", nil)
		svc.publishSnapshot()
		svc.flush()
		close(published)
		<-release
		close(actionDone)
	})
	select {
	case <-published:
	case <-time.After(2 * time.Second):
		t.Fatal("event-loop publication did not complete")
	}
	for range 2 {
		ev := nextPublished(t, w)
		if ev.Current == nil {
			t.Fatalf("unexpected publication: %+v", ev)
		}
		for _, key := range []string{"first", "second"} {
			if _, err := svc.Get(key); err != nil {
				t.Fatalf("notification preceded whole snapshot for %q: %v", key, err)
			}
		}
	}
	select {
	case <-actionDone:
		t.Fatal("action returned before watcher inspected its publication")
	default:
	}
}

func TestServiceWatchDeletionSeesWholePublishedSnapshot(t *testing.T) {
	for _, operation := range []string{"delete", "compare-delete", "transaction", "revoke", "expiry"} {
		t.Run(operation, func(t *testing.T) {
			svc := startPublishedService(t)
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
			w := watchPublished(t, svc)
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
				err = svc.submitAndWait(func() error {
					svc.leases.renew(lease.ID(), time.Now().Add(-2*lease.TTL()))
					svc.processExpiredLeases()
					return nil
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			for range keys {
				ev := nextPublished(t, w)
				if ev.Previous == nil || string(ev.Previous.Value) != ev.Previous.Key {
					t.Fatalf("deletion lost prior value: %+v", ev)
				}
				for _, key := range keys {
					if _, err := svc.Get(key); !errors.Is(err, kvapi.ErrKeyNotFound) {
						t.Fatalf("%s remained visible during %s: %v", key, operation, err)
					}
				}
			}
		})
	}
}

func TestServiceWatchTxnPreservesIntermediateEvents(t *testing.T) {
	svc := startPublishedService(t)
	w := watchPublished(t, svc)
	ok, err := svc.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnPut, Key: "key", Value: []byte("first")},
		{Kind: kvapi.TxnDelete, Key: "key"},
		{Kind: kvapi.TxnPut, Key: "key", Value: []byte("final")},
	})
	if err != nil || !ok {
		t.Fatalf("transaction: committed=%v err=%v", ok, err)
	}
	var events [3]kvapi.WatchEvent
	for i := range events {
		events[i] = nextPublished(t, w)
		current, err := svc.Get("key")
		if err != nil || string(current.Value) != "final" {
			t.Fatalf("event saw incomplete transaction: current=%+v err=%v", current, err)
		}
	}
	if events[0].Current == nil || string(events[0].Current.Value) != "first" ||
		events[1].Type != kvapi.WatchDelete || events[1].Previous == nil || string(events[1].Previous.Value) != "first" ||
		events[2].Current == nil || string(events[2].Current.Value) != "final" || events[2].Previous != nil {
		t.Fatalf("intermediate event values were overwritten: %+v", events)
	}
	ok, err = svc.Txn([]kvapi.TxnOp{{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: "key", Value: []byte("rejected")}})
	if err != nil || ok {
		t.Fatalf("failed transaction: committed=%v err=%v", ok, err)
	}
	// A failed transaction must have enqueued nothing even if the delivery
	// worker has not yet returned from the final successful notification.
	sub := w.(*watchSubscription)
	svc.watch.mu.Lock()
	queued := sub.count
	svc.watch.mu.Unlock()
	if queued != 0 {
		t.Fatalf("failed transaction queued %d notifications", queued)
	}
}
