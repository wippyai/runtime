// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"errors"
	"testing"
	"time"

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

func newTestStoreWithInfo(t *testing.T, namespace string, info store.Info) *Store {
	t.Helper()
	eng := systemkv.NewService(namespace, eventbus.NewBus(), zap.NewNop())
	if _, err := eng.Start(context.Background()); err != nil {
		t.Fatalf("engine start: %v", err)
	}
	t.Cleanup(func() { _ = eng.Stop(context.Background()) })
	return NewStoreWithInfo(registry.ParseID("app:"+namespace), namespace, eng, payloadSystem.GlobalTranscoder(), zap.NewNop(), info)
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

func TestStore_InfoListAndPut(t *testing.T) {
	ctx := context.Background()
	s := newTestStoreWithInfo(t, "rich", store.Info{
		Backend:        store.BackendKVRaft,
		Consistency:    store.ConsistencyLinearizable,
		Durable:        true,
		List:           true,
		Versioned:      true,
		ConditionalPut: true,
		TTL:            true,
	})

	info := s.StoreInfo(ctx)
	if info.Backend != store.BackendKVRaft || info.Consistency != store.ConsistencyLinearizable || !info.Durable || !info.ConditionalPut {
		t.Fatalf("unexpected info: %+v", info)
	}

	first, err := s.Put(ctx, registry.ParseID("app:dep-b"), jsonVal(`"b"`), store.PutOptions{OnlyIfAbsent: true})
	if err != nil {
		t.Fatalf("put absent: %v", err)
	}
	if first.Version == 0 {
		t.Fatalf("put version should be non-zero")
	}
	if _, err := s.Put(ctx, registry.ParseID("app:bad-ttl"), jsonVal(`"bad"`), store.PutOptions{TTL: -time.Second}); !errors.Is(err, store.ErrInvalidOptions) {
		t.Fatalf("put negative ttl = %v, want ErrInvalidOptions", err)
	}
	if _, err := s.Put(ctx, registry.ParseID("app:dep-b"), jsonVal(`"dupe"`), store.PutOptions{OnlyIfAbsent: true}); !errors.Is(err, store.ErrKeyExists) {
		t.Fatalf("put absent existing = %v, want ErrKeyExists", err)
	}
	if _, err := s.Put(ctx, registry.ParseID("app:dep-b"), jsonVal(`"bad"`), store.PutOptions{HasVersion: true, Version: first.Version + 100}); !errors.Is(err, store.ErrVersionMismatch) {
		t.Fatalf("put wrong version = %v, want ErrVersionMismatch", err)
	}

	second, err := s.Put(ctx, registry.ParseID("app:dep-b"), jsonVal(`"updated"`), store.PutOptions{HasVersion: true, Version: first.Version})
	if err != nil {
		t.Fatalf("put cas: %v", err)
	}
	if second.Version <= first.Version {
		t.Fatalf("cas version = %d, want > %d", second.Version, first.Version)
	}

	if _, err := s.Put(ctx, registry.ParseID("app:dep-a"), jsonVal(`"a"`), store.PutOptions{}); err != nil {
		t.Fatalf("put dep-a: %v", err)
	}
	page, err := s.List(ctx, store.ListOptions{Prefix: "app:dep-", Limit: 1})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Key.String() != "app:dep-a" || page.Cursor != "app:dep-a" || !page.HasMore {
		t.Fatalf("unexpected page: %+v", page)
	}
}

func TestStore_CRDTInfoGuardsConditionalPut(t *testing.T) {
	ctx := context.Background()
	s := newTestStoreWithInfo(t, "crdt-guard", store.Info{
		Backend:        store.BackendKVCRDT,
		Consistency:    store.ConsistencyEventual,
		Durable:        false,
		List:           true,
		Versioned:      true,
		ConditionalPut: false,
		TTL:            true,
	})

	info := s.StoreInfo(ctx)
	if info.Backend != store.BackendKVCRDT || info.Consistency != store.ConsistencyEventual || info.ConditionalPut {
		t.Fatalf("unexpected info: %+v", info)
	}
	if _, err := s.Put(ctx, registry.ParseID("app:k"), jsonVal(`"v"`), store.PutOptions{OnlyIfAbsent: true}); !errors.Is(err, store.ErrUnsupported) {
		t.Fatalf("conditional crdt put = %v, want ErrUnsupported", err)
	}
	if _, err := s.Put(ctx, registry.ParseID("app:k"), jsonVal(`"v"`), store.PutOptions{}); err != nil {
		t.Fatalf("unconditional crdt put should work: %v", err)
	}
}
