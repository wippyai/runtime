// SPDX-License-Identifier: MPL-2.0

package kveventual

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/kv"
)

type stubPeers struct{ peers []string }

func (p *stubPeers) AlivePeers() []string { return p.peers }

func newTestService(t *testing.T) *Service {
	t.Helper()
	svc := NewService(Config{
		LocalNodeID: "node-A",
		Peers:       &stubPeers{},
	})
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop() })
	return svc
}

func TestService_PutGet(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	space, err := svc.Open("test")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := space.Put(ctx, "k", []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	v, err := space.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(v.Data) != "v" {
		t.Errorf("got %q want v", v.Data)
	}
}

func TestService_GetMissingReturnsErrKeyNotFound(t *testing.T) {
	svc := newTestService(t)
	space, _ := svc.Open("test")
	_, err := space.Get(context.Background(), "missing")
	if !errors.Is(err, kv.ErrKeyNotFound) {
		t.Errorf("err=%v want ErrKeyNotFound", err)
	}
}

func TestService_DeleteIsIdempotent(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	space, _ := svc.Open("test")
	if err := space.Delete(ctx, "missing"); err != nil {
		t.Errorf("delete missing: %v", err)
	}
	_ = space.Put(ctx, "k", []byte("v"))
	if err := space.Delete(ctx, "k"); err != nil {
		t.Errorf("delete present: %v", err)
	}
	if _, err := space.Get(ctx, "k"); !errors.Is(err, kv.ErrKeyNotFound) {
		t.Errorf("after delete: err=%v", err)
	}
}

func TestService_CASReturnsUnsupported(t *testing.T) {
	svc := newTestService(t)
	space, _ := svc.Open("test")
	err := space.CompareAndSwap(context.Background(), "k", []byte("a"), []byte("b"))
	if !errors.Is(err, kv.ErrUnsupported) {
		t.Errorf("err=%v want ErrUnsupported", err)
	}
}

func TestService_PutWithExpectVersionUnsupported(t *testing.T) {
	svc := newTestService(t)
	space, _ := svc.Open("test")
	err := space.Put(context.Background(), "k", []byte("v"), kv.WithExpectVersion(1))
	if !errors.Is(err, kv.ErrUnsupported) {
		t.Errorf("err=%v want ErrUnsupported", err)
	}
}

func TestService_PutWithExpectAbsent(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	space, _ := svc.Open("test")
	if err := space.Put(ctx, "k", []byte("v"), kv.WithExpectAbsent()); err != nil {
		t.Fatalf("first put: %v", err)
	}
	if err := space.Put(ctx, "k", []byte("v2"), kv.WithExpectAbsent()); !errors.Is(err, kv.ErrKeyExists) {
		t.Errorf("second put err=%v want ErrKeyExists", err)
	}
}

func TestService_Watch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := newTestService(t)
	space, _ := svc.Open("test")

	ch, err := space.Watch(ctx, "user:")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	if err := space.Put(ctx, "user:1", []byte("alice")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	select {
	case ev := <-ch:
		if ev.Key != "user:1" || string(ev.Value.Data) != "alice" || ev.Op != kv.OpPut {
			t.Errorf("event mismatch: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no watch event received")
	}

	// Non-matching prefix shouldn't be delivered.
	_ = space.Put(ctx, "other:1", []byte("ignored"))
	select {
	case ev := <-ch:
		t.Errorf("unexpected event for non-matching key: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestService_OpenSameSpaceReturnsSameHandle(t *testing.T) {
	svc := newTestService(t)
	a, _ := svc.Open("shared")
	b, _ := svc.Open("shared")
	if a != b {
		t.Errorf("expected same handle for same space")
	}
}

func TestService_Scan(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	space, _ := svc.Open("test")
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		_ = space.Put(ctx, k, []byte(k))
	}
	var seen []string
	_ = space.Scan(ctx, "b", "e", func(k string, _ kv.Value) bool {
		seen = append(seen, k)
		return true
	})
	want := []string{"b", "c", "d"}
	if len(seen) != len(want) {
		t.Errorf("seen=%v want=%v", seen, want)
	}
	for i, k := range want {
		if i >= len(seen) || seen[i] != k {
			t.Errorf("seen[%d]=%q want=%q", i, seen[i], k)
		}
	}
}
