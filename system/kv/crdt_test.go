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

func newCRDT(t *testing.T, node string) *CRDTEngine {
	t.Helper()
	e := NewCRDTEngine(node, eventbus.NewBus(), zap.NewNop())
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop() })
	return e
}

const crdtBudget = 1 << 20

// gossip drains src's outbound deltas into dst.
func gossip(src, dst *CRDTEngine) {
	for _, f := range src.DrainBroadcasts(0, crdtBudget) {
		dst.OnFrame(f)
	}
}

func TestCRDTEngine_BasicOps(t *testing.T) {
	e := newCRDT(t, "n1")

	if _, err := e.Set("k", []byte("v")); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := e.Get("k")
	if err != nil || string(got.Value) != "v" {
		t.Fatalf("get = %+v err=%v", got, err)
	}
	if _, stored, _ := e.SetIfAbsent("k", []byte("z")); stored {
		t.Fatalf("setIfAbsent existing should not store")
	}
	if _, stored, _ := e.SetIfAbsent("n", []byte("z")); !stored {
		t.Fatalf("setIfAbsent new should store")
	}
	ve, _ := e.Get("k")
	if _, ok, _ := e.CompareAndSwap("k", ve.Version, []byte("v2")); !ok {
		t.Fatalf("cas should succeed")
	}
	if _, ok, _ := e.CompareAndSwap("k", 999, []byte("x")); ok {
		t.Fatalf("cas wrong version should fail")
	}
	if err := e.Delete("k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := e.Get("k"); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("get after delete = %v", err)
	}
}

// TestCRDTEngine_Converges checks that deltas exchanged both ways converge, and
// concurrent conflicting writes resolve to the same value on both replicas.
func TestCRDTEngine_Converges(t *testing.T) {
	a := newCRDT(t, "nA")
	b := newCRDT(t, "nB")

	if _, err := a.Set("x", []byte("from-a")); err != nil {
		t.Fatalf("a.set: %v", err)
	}
	gossip(a, b)
	if got, _ := b.Get("x"); string(got.Value) != "from-a" {
		t.Fatalf("b did not converge on x: %q", got.Value)
	}

	// Concurrent conflicting writes to the same key.
	_, _ = a.Set("c", []byte("a-wins?"))
	_, _ = b.Set("c", []byte("b-wins?"))
	gossip(a, b)
	gossip(b, a)
	// A second round settles any re-broadcast.
	gossip(a, b)
	gossip(b, a)

	av, _ := a.Get("c")
	bv, _ := b.Get("c")
	if string(av.Value) != string(bv.Value) {
		t.Fatalf("replicas diverged on c: a=%q b=%q", av.Value, bv.Value)
	}
}

// TestCRDTEngine_DelegateAntiEntropy checks the full-state push/pull path: a
// fresh replica that merges another's LocalState converges without deltas.
func TestCRDTEngine_DelegateAntiEntropy(t *testing.T) {
	a := newCRDT(t, "nA")
	b := newCRDT(t, "nB")
	_, _ = a.Set("x", []byte("v"))
	_, _ = a.Set("y", []byte("w"))

	da := NewCRDTDelegate(a)
	db := NewCRDTDelegate(b)
	db.MergeRemoteState(da.LocalState(true), true)

	if got, _ := b.Get("x"); string(got.Value) != "v" {
		t.Fatalf("b.x = %q after push/pull", got.Value)
	}
	if got, _ := b.Get("y"); string(got.Value) != "w" {
		t.Fatalf("b.y = %q after push/pull", got.Value)
	}
}

// TestCRDTEngine_DurablePersistsOnlyMarkedNamespaces verifies durable namespaces
// survive a restart while ephemeral ones do not.
func TestCRDTEngine_DurablePersistsOnlyMarkedNamespaces(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	e1 := NewCRDTEngine("n1", eventbus.NewBus(), zap.NewNop())
	e1.SetDurability(dir, time.Hour)
	e1.MarkDurable("dur")
	if err := e1.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	_, _ = e1.Set("dur:k", []byte("keep"))
	_, _ = e1.Set("eph:k", []byte("drop"))
	if err := e1.snapshotDurable(); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	_ = e1.Stop()

	e2 := NewCRDTEngine("n1", eventbus.NewBus(), zap.NewNop())
	e2.SetDurability(dir, time.Hour)
	e2.MarkDurable("dur")
	if err := e2.Start(ctx); err != nil {
		t.Fatalf("restart: %v", err)
	}
	t.Cleanup(func() { _ = e2.Stop() })

	if got, err := e2.Get("dur:k"); err != nil || string(got.Value) != "keep" {
		t.Fatalf("durable key lost after restart: %+v err=%v", got, err)
	}
	if _, err := e2.Get("eph:k"); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("ephemeral key should not survive restart, got err=%v", err)
	}
}

// TestCRDTEngine_DurableTombstoneSurvivesRestartNoResurrection proves a deleted
// durable key stays deleted across a restart (the tombstone is persisted, not
// dropped to a live value) AND that re-gossiping the restored node's loaded
// state does not resurrect the key on a peer that still holds it live — the
// delete (higher per-node counter than the original set) wins everywhere.
func TestCRDTEngine_DurableTombstoneSurvivesRestartNoResurrection(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	e1 := NewCRDTEngine("n1", eventbus.NewBus(), zap.NewNop())
	e1.SetDurability(dir, time.Hour)
	e1.MarkDurable("dur")
	if err := e1.Start(ctx); err != nil {
		t.Fatalf("start e1: %v", err)
	}

	// A peer that learns the key live before it is deleted.
	peer := newCRDT(t, "n3")
	peer.MarkDurable("dur")

	if _, err := e1.Set("dur:k", []byte("v")); err != nil {
		t.Fatalf("set: %v", err)
	}
	gossip(e1, peer)
	if got, err := peer.Get("dur:k"); err != nil || string(got.Value) != "v" {
		t.Fatalf("peer must learn the live key first: %+v err=%v", got, err)
	}

	if err := e1.Delete("dur:k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := e1.snapshotDurable(); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	_ = e1.Stop()

	// Restart from disk: the tombstone must be restored, not resurrected as live.
	e2 := NewCRDTEngine("n1", eventbus.NewBus(), zap.NewNop())
	e2.SetDurability(dir, time.Hour)
	e2.MarkDurable("dur")
	if err := e2.Start(ctx); err != nil {
		t.Fatalf("restart e2: %v", err)
	}
	t.Cleanup(func() { _ = e2.Stop() })

	if _, err := e2.Get("dur:k"); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("deleted durable key must stay deleted after restart, got err=%v", err)
	}

	// Re-gossiping the restored node must carry the tombstone and delete the
	// peer's still-live copy — no resurrection in either direction.
	gossip(e2, peer)
	if _, err := peer.Get("dur:k"); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("peer's live copy must be deleted by the restored tombstone, got err=%v", err)
	}
	gossip(peer, e2)
	if _, err := e2.Get("dur:k"); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("restored node must not be resurrected by peer gossip, got err=%v", err)
	}
}

func TestCRDTEngine_LeaseExpiry(t *testing.T) {
	e := newCRDT(t, "n1")
	lease, err := e.GrantLease(context.Background(), 50*time.Millisecond)
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if _, err := e.SetWithLease("lk", []byte("lv"), lease.ID()); err != nil {
		t.Fatalf("set with lease: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := e.Get("lk"); errors.Is(err, kvapi.ErrKeyNotFound) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("leased key not expired by reaper")
}
