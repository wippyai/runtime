// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"errors"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/store"
	"github.com/wippyai/runtime/system/eventbus"
	systemkv "github.com/wippyai/runtime/system/kv"
	payloadSystem "github.com/wippyai/runtime/system/payload"
	"go.uber.org/zap"
)

func newTestStore(t *testing.T, namespace string) *Store {
	t.Helper()
	eng := systemkv.NewService(namespace, eventbus.NewBus(), zap.NewNop())
	if _, err := eng.Start(context.Background()); err != nil {
		t.Fatalf("engine start: %v", err)
	}
	t.Cleanup(func() { _ = eng.Stop(context.Background()) })
	return NewStore(registry.ParseID("app:"+namespace), namespace, eng, payloadSystem.GlobalTranscoder(), zap.NewNop())
}

func jsonVal(s string) payload.Payload { return payload.NewPayload([]byte(s), payload.JSON) }

func TestStore_SetGetDeleteHas(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "cache")
	key := registry.ParseID("app:k1")

	if err := s.Set(ctx, store.Entry{Key: key, Value: jsonVal(`"v1"`)}); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if b, _ := got.Data().([]byte); string(b) != `"v1"` {
		t.Fatalf("get value = %q", b)
	}
	if ok, _ := s.Has(ctx, key); !ok {
		t.Fatalf("has should be true")
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(ctx, key); !errors.Is(err, store.ErrKeyNotFound) {
		t.Fatalf("get after delete = %v, want ErrKeyNotFound", err)
	}
	if ok, _ := s.Has(ctx, key); ok {
		t.Fatalf("has should be false after delete")
	}
}

// TestStore_NamespaceIsolation ensures two stores on the same engine with
// different namespaces cannot see each other's keys.
func TestStore_NamespaceIsolation(t *testing.T) {
	ctx := context.Background()
	eng := systemkv.NewService("shared", eventbus.NewBus(), zap.NewNop())
	if _, err := eng.Start(ctx); err != nil {
		t.Fatalf("engine start: %v", err)
	}
	t.Cleanup(func() { _ = eng.Stop(ctx) })
	dtt := payloadSystem.GlobalTranscoder()

	a := NewStore(registry.ParseID("app:a"), "nsA", eng, dtt, zap.NewNop())
	b := NewStore(registry.ParseID("app:b"), "nsB", eng, dtt, zap.NewNop())

	key := registry.ParseID("app:same")
	if err := a.Set(ctx, store.Entry{Key: key, Value: jsonVal(`"fromA"`)}); err != nil {
		t.Fatalf("a.set: %v", err)
	}
	if ok, _ := b.Has(ctx, key); ok {
		t.Fatalf("namespace B must not see namespace A's key")
	}
	if _, err := b.Get(ctx, key); !errors.Is(err, store.ErrKeyNotFound) {
		t.Fatalf("b.get = %v, want ErrKeyNotFound", err)
	}

	// A scan in B must not surface A's entries.
	n := 0
	_ = b.Scan(ctx, store.ScanOptions{}, func(store.Entry) bool { n++; return true })
	if n != 0 {
		t.Fatalf("b.scan saw %d entries from another namespace", n)
	}
}

func TestStore_AtomicAndScan(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "cas")
	key := registry.ParseID("app:k")

	stored, err := s.SetIfAbsent(ctx, store.Entry{Key: key, Value: jsonVal(`"first"`)})
	if err != nil || !stored {
		t.Fatalf("setIfAbsent new: stored=%v err=%v", stored, err)
	}
	if stored, _ := s.SetIfAbsent(ctx, store.Entry{Key: key, Value: jsonVal(`"second"`)}); stored {
		t.Fatalf("setIfAbsent existing should not store")
	}

	ve, err := s.GetVersioned(ctx, key)
	if err != nil {
		t.Fatalf("getVersioned: %v", err)
	}
	ok, err := s.CompareAndSwap(ctx, key, ve.Version, store.Entry{Key: key, Value: jsonVal(`"swapped"`)})
	if err != nil || !ok {
		t.Fatalf("cas: ok=%v err=%v", ok, err)
	}
	if bad, _ := s.CompareAndSwap(ctx, key, 999, store.Entry{Key: key, Value: jsonVal(`"x"`)}); bad {
		t.Fatalf("cas wrong version should fail")
	}

	_ = s.Set(ctx, store.Entry{Key: registry.ParseID("app:p1"), Value: jsonVal(`1`)})
	_ = s.Set(ctx, store.Entry{Key: registry.ParseID("app:p2"), Value: jsonVal(`2`)})
	n := 0
	if err := s.Scan(ctx, store.ScanOptions{Prefix: "app:p"}, func(store.Entry) bool { n++; return true }); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 2 {
		t.Fatalf("scan prefix found %d, want 2", n)
	}
}
