// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
	"go.uber.org/zap"
)

// esender adapts a membership.Service to eventual.MessageSender (the shard-pull
// reliable channel), mirroring boot/components/system/eventualreg.go.
type esender struct{ m *Service }

func (s esender) Send(target string, payload []byte) error {
	return s.m.SendUserMessage(target, eventual.DelegateKind, payload)
}

// epeers adapts a membership.Service to eventual.PeerInventory.
type epeers struct{ m *Service }

func (p epeers) AlivePeers() []string {
	self := p.m.LocalNode().ID
	var out []string
	for _, n := range p.m.Nodes() {
		if n.ID != self {
			out = append(out, n.ID)
		}
	}
	return out
}

type eNode struct {
	m  *Service
	es *eventual.Service
}

func newEventualNode(ctx context.Context, t *testing.T, name string, join ...string) *eNode {
	t.Helper()
	bus := eventbus.NewBus()
	logger := zap.NewNop()
	cfg := Config{NodeName: name, BindAddr: "127.0.0.1", BindPort: 0, JoinAddrs: join}
	m := NewService(cfg, bus, logger.Named(name), nil, nil, nil)
	es := eventual.NewService(eventual.Config{
		LocalNodeID: name,
		Sender:      esender{m},
		Peers:       epeers{m},
		Bus:         bus,
		Logger:      logger.Named(name),
	})
	if err := m.RegisterUserDelegate(eventual.NewDelegate(es, logger)); err != nil {
		t.Fatalf("%s: register delegate: %v", name, err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatalf("%s: start membership: %v", name, err)
	}
	if err := es.Start(ctx); err != nil {
		t.Fatalf("%s: start eventual: %v", name, err)
	}
	return &eNode{m: m, es: es}
}

func (n *eNode) addr() string {
	ln := n.m.memberlist.LocalNode()
	return fmt.Sprintf("%s:%d", ln.Addr, ln.Port)
}

func (n *eNode) stop() {
	_ = n.es.Stop()
	_ = n.m.Stop()
}

func waitMembers(t *testing.T, d time.Duration, want int, nodes ...*eNode) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		ok := true
		for _, n := range nodes {
			if n.m.memberlist.NumMembers() < want {
				ok = false
				break
			}
		}
		if ok {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, n := range nodes {
		t.Logf("node %s sees %d members", n.m.config.NodeName, n.m.memberlist.NumMembers())
	}
	t.Fatalf("membership did not converge to %d", want)
}

func resolves(es *eventual.Service, name string, want pid.PID) bool {
	res, err := es.Lookup(context.Background(), name)
	return err == nil && res.Found && res.PID == want
}

func waitResolve(t *testing.T, d time.Duration, es *eventual.Service, name string, want pid.PID) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if resolves(es, name, want) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// TestEventual_LateJoinerConvergesViaAntiEntropy reproduces the reported bug:
// a name registered on a server BEFORE a client joins must still reach the
// client. The delta is a one-shot broadcast drained before the client exists,
// so the only path to the client is push/pull anti-entropy.
func TestEventual_LateJoinerConvergesViaAntiEntropy(t *testing.T) {
	if testing.Short() {
		t.Skip("loopback multi-node test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	srv1 := newEventualNode(ctx, t, "srv-1")
	defer srv1.stop()
	srv2 := newEventualNode(ctx, t, "srv-2", srv1.addr())
	defer srv2.stop()
	waitMembers(t, 15*time.Second, 2, srv1, srv2)

	p := pid.PID{Node: "srv-1", Host: "h", UniqID: "sx"}
	if got, err := srv1.es.Register("session.x", p); err != nil {
		t.Fatalf("register on srv-1: %v (got %v)", err, got)
	}

	// Sanity: server-to-server delta path works.
	if !waitResolve(t, 15*time.Second, srv2.es, "session.x", p) {
		t.Fatalf("srv-2 never learned session.x via delta gossip (server-server path broken)")
	}
	t.Logf("server-server converged; starting late-joining client")

	// Client joins AFTER the delta was drained — only anti-entropy can deliver.
	start := time.Now()
	client := newEventualNode(ctx, t, "client-1", srv1.addr())
	defer client.stop()
	waitMembers(t, 15*time.Second, 3, srv1, srv2, client)

	if !waitResolve(t, 30*time.Second, client.es, "session.x", p) {
		t.Fatalf("BUG REPRODUCED: client never resolved session.x via anti-entropy (waited %s)", time.Since(start))
	}
	t.Logf("client converged via anti-entropy after %s", time.Since(start))
}

// TestEventual_PostJoinPropagatesToAllNodes is the regression for the reported
// bug: in a cluster larger than GossipNodes, a name registered after everyone
// joined must reach every node promptly via epidemic forwarding + anti-entropy.
// Pre-fix the tail took tens of seconds (one-shot delta to 3 peers, no
// forwarding, slow 15s push/pull); post-fix it is a few seconds at most.
func TestEventual_PostJoinPropagatesToAllNodes(t *testing.T) {
	if testing.Short() {
		t.Skip("loopback multi-node test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	const n = 7
	nodes := []*eNode{newEventualNode(ctx, t, "n0")}
	defer func() {
		for _, nd := range nodes {
			nd.stop()
		}
	}()
	seed := nodes[0].addr()
	for i := 1; i < n; i++ {
		nodes = append(nodes, newEventualNode(ctx, t, fmt.Sprintf("n%d", i), seed))
	}
	waitMembers(t, 30*time.Second, n, nodes...)

	p := pid.PID{Node: "n0", Host: "h", UniqID: "px"}
	if _, err := nodes[0].es.Register("svc.x", p); err != nil {
		t.Fatalf("register: %v", err)
	}

	start := time.Now()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		all := true
		for _, nd := range nodes {
			if !resolves(nd.es, "svc.x", p) {
				all = false
				break
			}
		}
		if all {
			t.Logf("all %d nodes converged in %s", n, time.Since(start))
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	missing := 0
	for _, nd := range nodes {
		if !resolves(nd.es, "svc.x", p) {
			missing++
		}
	}
	t.Fatalf("BUG: %d/%d nodes still missing svc.x after %s", missing, n, time.Since(start))
}
