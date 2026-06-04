// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
)

// TestEventual_ClientPublishResolvedByServers proves the publish direction for a
// raft-less node: a late-joining client REGISTERS an eventual name and every
// server must resolve it. The client's delta is a one-shot broadcast, so the
// servers learn it only via epidemic forwarding + anti-entropy — the same path
// the fix repairs, exercised from the client outward instead of inward.
func TestEventual_ClientPublishResolvedByServers(t *testing.T) {
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

	client := newEventualNode(ctx, t, "client-1", srv1.addr())
	defer client.stop()
	waitMembers(t, 15*time.Second, 3, srv1, srv2, client)

	cp := pid.PID{Node: "client-1", Host: "h", UniqID: "cx"}
	if got, err := client.es.Register("client.session", cp); err != nil {
		t.Fatalf("register on client: %v (got %v)", err, got)
	}

	if !waitResolve(t, 30*time.Second, srv1.es, "client.session", cp) {
		t.Fatalf("srv-1 never resolved the client-published name (client->server publish path broken)")
	}
	if !waitResolve(t, 30*time.Second, srv2.es, "client.session", cp) {
		t.Fatalf("srv-2 never resolved the client-published name (epidemic forward to tail broken)")
	}
}

// TestEventual_ClientServerBidirectional proves both directions converge at once:
// a raft-less client and a server each register a name, and every node resolves
// both. This is the full "eventual name reg works across non-raft nodes" claim:
// a client both publishes its own names AND resolves others'.
func TestEventual_ClientServerBidirectional(t *testing.T) {
	if testing.Short() {
		t.Skip("loopback multi-node test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	srv1 := newEventualNode(ctx, t, "srv-1")
	defer srv1.stop()
	srv2 := newEventualNode(ctx, t, "srv-2", srv1.addr())
	defer srv2.stop()
	client := newEventualNode(ctx, t, "client-1", srv1.addr())
	defer client.stop()
	waitMembers(t, 15*time.Second, 3, srv1, srv2, client)

	srvPID := pid.PID{Node: "srv-1", Host: "h", UniqID: "sx"}
	clientPID := pid.PID{Node: "client-1", Host: "h", UniqID: "cx"}
	if _, err := srv1.es.Register("svc.api", srvPID); err != nil {
		t.Fatalf("register on srv-1: %v", err)
	}
	if _, err := client.es.Register("svc.worker", clientPID); err != nil {
		t.Fatalf("register on client: %v", err)
	}

	nodes := map[string]*eNode{"srv-1": srv1, "srv-2": srv2, "client-1": client}
	for name, n := range nodes {
		if !waitResolve(t, 30*time.Second, n.es, "svc.api", srvPID) {
			t.Fatalf("%s never resolved server-published svc.api", name)
		}
		if !waitResolve(t, 30*time.Second, n.es, "svc.worker", clientPID) {
			t.Fatalf("%s never resolved client-published svc.worker", name)
		}
	}
}
