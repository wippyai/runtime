// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"errors"
	"testing"
	"time"

	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// TestCRDTEngine_DurableNamespaceReloads covers SetDurability (interval > 0 vs
// 0), the durable-namespace prefix filter, and the snapshot/reload path: only
// keys in a marked namespace survive a restart.
func TestCRDTEngine_DurableNamespaceReloads(t *testing.T) {
	dir := t.TempDir()

	e1 := NewCRDTEngine("n1", eventbus.NewBus(), zap.NewNop())
	e1.SetDurability(dir, 50*time.Millisecond)
	if e1.snapInterval != 50*time.Millisecond {
		t.Fatalf("SetDurability(interval>0) snapInterval = %v, want 50ms", e1.snapInterval)
	}
	e1.MarkDurable("d")
	if err := e1.Start(context.Background()); err != nil {
		t.Fatalf("start e1: %v", err)
	}
	if _, err := e1.Set("d:k", []byte("v")); err != nil {
		t.Fatalf("set durable: %v", err)
	}
	if _, err := e1.Set("x:k", []byte("v")); err != nil {
		t.Fatalf("set non-durable: %v", err)
	}
	if err := e1.snapshotDurable(); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	_ = e1.Stop()

	e2 := NewCRDTEngine("n1", eventbus.NewBus(), zap.NewNop())
	e2.SetDurability(dir, 0)
	if e2.snapInterval != 30*time.Second {
		t.Fatalf("SetDurability(interval==0) must keep default, got %v", e2.snapInterval)
	}
	e2.MarkDurable("d")
	if err := e2.Start(context.Background()); err != nil {
		t.Fatalf("start e2: %v", err)
	}
	t.Cleanup(func() { _ = e2.Stop() })

	if got, err := e2.Get("d:k"); err != nil || string(got.Value) != "v" {
		t.Fatalf("durable key not reloaded: %+v err=%v", got, err)
	}
	if _, err := e2.Get("x:k"); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("non-durable key wrongly persisted: %v", err)
	}
}

// TestCRDTEngine_OnFrameEmitsAndApplies covers the inbound-delta path: a frame
// that advances local state applies the value and emits a watch event.
func TestCRDTEngine_OnFrameEmitsAndApplies(t *testing.T) {
	src := newCRDT(t, "src")
	dst := newCRDT(t, "dst")

	w, err := dst.Watch(context.Background(), "")
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	if _, err := src.Set("k", []byte("v")); err != nil {
		t.Fatalf("src set: %v", err)
	}
	gossip(src, dst)

	if got, err := dst.Get("k"); err != nil || string(got.Value) != "v" {
		t.Fatalf("dst did not apply gossiped delta: %+v err=%v", got, err)
	}

	select {
	case ev := <-w.Events():
		if ev.Type != kvapi.WatchPut || ev.Current == nil || ev.Current.Key != "k" {
			t.Fatalf("expected WatchPut for k from gossip, got %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no watch event delivered after gossiped delta")
	}
}
