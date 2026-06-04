// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/wippyai/runtime/system/eventbus"
	systemkv "github.com/wippyai/runtime/system/kv"
	"go.uber.org/zap"
)

type crdtNode struct {
	m   *Service
	eng *systemkv.CRDTEngine
}

func newCRDTGossipNode(ctx context.Context, t *testing.T, name string, join ...string) *crdtNode {
	t.Helper()
	bus := eventbus.NewBus()
	eng := systemkv.NewCRDTEngine(name, bus, zap.NewNop())
	if err := eng.Start(ctx); err != nil {
		t.Fatalf("%s engine start: %v", name, err)
	}
	m := NewService(Config{NodeName: name, BindAddr: "127.0.0.1", BindPort: 0, JoinAddrs: join},
		bus, zap.NewNop().Named(name), nil, nil, nil)
	if err := m.RegisterUserDelegate(systemkv.NewCRDTDelegate(eng)); err != nil {
		t.Fatalf("%s register delegate: %v", name, err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatalf("%s membership start: %v", name, err)
	}
	return &crdtNode{m: m, eng: eng}
}

func (n *crdtNode) addr() string {
	ln := n.m.memberlist.LocalNode()
	return fmt.Sprintf("%s:%d", ln.Addr, ln.Port)
}

func (n *crdtNode) stop() {
	_ = n.eng.Stop()
	_ = n.m.Stop()
}

func waitConv(t *testing.T, eng *systemkv.CRDTEngine, key, want string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if e, err := eng.Get(key); err == nil && string(e.Value) == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("key %q did not converge to %q within %s", key, want, d)
}

// TestCRDTGossip_ConvergesOverRealMemberlist proves the crdt delegate propagates
// writes to all nodes over a real memberlist cluster (delta + anti-entropy),
// including a node that joins after the write (anti-entropy backfill).
func TestCRDTGossip_ConvergesOverRealMemberlist(t *testing.T) {
	if testing.Short() {
		t.Skip("loopback gossip test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	a := newCRDTGossipNode(ctx, t, "a")
	defer a.stop()
	b := newCRDTGossipNode(ctx, t, "b", a.addr())
	defer b.stop()

	// Wait for 2-node membership.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && (a.m.memberlist.NumMembers() < 2 || b.m.memberlist.NumMembers() < 2) {
		time.Sleep(50 * time.Millisecond)
	}

	if _, err := a.eng.Set("k", []byte("v1")); err != nil {
		t.Fatalf("set: %v", err)
	}
	waitConv(t, b.eng, "k", "v1", 15*time.Second)

	// Write BEFORE a late joiner exists, then start it — it must backfill via
	// anti-entropy (full-state push/pull).
	if _, err := a.eng.Set("late", []byte("v2")); err != nil {
		t.Fatalf("set late: %v", err)
	}
	c := newCRDTGossipNode(ctx, t, "c", a.addr())
	defer c.stop()
	waitConv(t, c.eng, "k", "v1", 30*time.Second)
	waitConv(t, c.eng, "late", "v2", 30*time.Second)
}
