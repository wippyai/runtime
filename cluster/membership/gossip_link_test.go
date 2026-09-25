// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

var errUnreachable = errors.New("destination unreachable")

// oneWayTransport cannot reach one node, as a host outside a NAT cannot reach
// a node behind it. That node can still reach this one.
type oneWayTransport struct {
	*memberlist.MockTransport
	unreachable memberlist.Address
}

func (t *oneWayTransport) blocked(addr memberlist.Address) bool {
	return addr.Name == t.unreachable.Name || addr.Addr == t.unreachable.Addr
}

func (t *oneWayTransport) WriteTo(b []byte, addr string) (time.Time, error) {
	return t.WriteToAddress(b, memberlist.Address{Addr: addr})
}

// WriteToAddress drops packets for the unreachable node and reports success,
// as UDP does when a NAT discards them.
func (t *oneWayTransport) WriteToAddress(b []byte, addr memberlist.Address) (time.Time, error) {
	if t.blocked(addr) {
		return time.Now(), nil
	}
	return t.MockTransport.WriteToAddress(b, addr)
}

func (t *oneWayTransport) DialTimeout(addr string, timeout time.Duration) (net.Conn, error) {
	return t.DialAddressTimeout(memberlist.Address{Addr: addr}, timeout)
}

func (t *oneWayTransport) DialAddressTimeout(addr memberlist.Address, timeout time.Duration) (net.Conn, error) {
	if t.blocked(addr) {
		return nil, errUnreachable
	}
	return t.MockTransport.DialAddressTimeout(addr, timeout)
}

// testLinks connects nodes the way established internode links do.
type testLinks struct {
	receivers map[string]func(string, []byte)
	mu        sync.Mutex
}

type testLink struct {
	links *testLinks
	self  string
}

func (l testLink) SendConnected(node string, data []byte, class internode.Class) bool {
	l.links.mu.Lock()
	recv := l.links.receivers[node]
	l.links.mu.Unlock()
	if recv == nil || class != internode.ClassGossip {
		return false
	}
	recv(l.self, data)
	return true
}

func (l testLink) RegisterClassReceiver(class internode.Class, recv func(string, []byte)) bool {
	if class != internode.ClassGossip {
		return false
	}
	l.links.mu.Lock()
	defer l.links.mu.Unlock()
	if recv != nil && l.links.receivers[l.self] != nil {
		return false
	}
	l.links.receivers[l.self] = recv
	return true
}

func fastProbeConfig(name string, transport memberlist.Transport, link GossipLink, join []string) Config {
	return Config{
		NodeName:         name,
		Transport:        transport,
		Link:             link,
		JoinAddrs:        join,
		GossipInterval:   10 * time.Millisecond,
		ProbeInterval:    20 * time.Millisecond,
		ProbeTimeout:     10 * time.Millisecond,
		PushPullInterval: time.Hour,
		TCPTimeout:       50 * time.Millisecond,
		SuspicionMult:    2,
	}
}

func hasNode(s *Service, name string) bool {
	for _, node := range s.Nodes() {
		if node.ID == name {
			return true
		}
	}
	return false
}

// startOneWayPair starts node-a, which cannot reach node-b, and node-b, which
// joins through node-a. links is nil when no internode link exists.
func startOneWayPair(ctx context.Context, t *testing.T, links *testLinks) (a, b *Service) {
	t.Helper()
	network := &memberlist.MockNetwork{}
	transportA := network.NewTransport("node-a")
	transportB := network.NewTransport("node-b")
	addrB, portB, err := transportB.FinalAdvertiseAddr("", 0)
	require.NoError(t, err)
	unreachable := memberlist.Address{Addr: net.JoinHostPort(addrB.String(), strconv.Itoa(portB)), Name: "node-b"}
	addrA, portA, err := transportA.FinalAdvertiseAddr("", 0)
	require.NoError(t, err)

	var linkA, linkB GossipLink
	if links != nil {
		linkA = testLink{links: links, self: "node-a"}
		linkB = testLink{links: links, self: "node-b"}
	}
	a = NewService(fastProbeConfig("node-a", &oneWayTransport{MockTransport: transportA, unreachable: unreachable}, linkA, nil),
		eventbus.NewBus(), zap.NewNop(), nil, nil, nil)
	require.NoError(t, a.Start(ctx))
	t.Cleanup(func() { _ = a.Stop() })
	b = NewService(fastProbeConfig("node-b", transportB, linkB, []string{net.JoinHostPort(addrA.String(), strconv.Itoa(portA))}),
		eventbus.NewBus(), zap.NewNop(), nil, nil, nil)
	require.NoError(t, b.Start(ctx))
	t.Cleanup(func() { _ = b.Stop() })

	require.Eventually(t, func() bool { return hasNode(a, "node-b") && hasNode(b, "node-a") }, 5*time.Second, 5*time.Millisecond)
	return a, b
}

// A node that cannot be reached directly stays a member when its internode
// link carries the probes.
func TestGossipLinkKeepsOneWayReachableNodeAlive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a, _ := startOneWayPair(ctx, t, &testLinks{receivers: map[string]func(string, []byte){}})
	require.Never(t, func() bool { return !hasNode(a, "node-b") }, 2*time.Second, 10*time.Millisecond)
}

// The same pair without a link loses the unreachable node, which is the
// failure the link exists to prevent.
func TestOneWayReachableNodeFailsWithoutGossipLink(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a, _ := startOneWayPair(ctx, t, nil)
	require.Eventually(t, func() bool { return !hasNode(a, "node-b") }, 5*time.Second, 10*time.Millisecond)
}

// blackholeLink accepts every packet and delivers none, as a link stalled
// behind large frames does.
type blackholeLink struct{}

func (blackholeLink) SendConnected(string, []byte, internode.Class) bool { return true }

func (blackholeLink) RegisterClassReceiver(internode.Class, func(string, []byte)) bool { return true }

// dialCountingTransport counts stream dials, which memberlist opens for its
// TCP fallback ping when a probe goes unanswered.
type dialCountingTransport struct {
	*memberlist.MockTransport
	dials atomic.Int32
}

func (t *dialCountingTransport) DialTimeout(addr string, timeout time.Duration) (net.Conn, error) {
	return t.DialAddressTimeout(memberlist.Address{Addr: addr}, timeout)
}

func (t *dialCountingTransport) DialAddressTimeout(addr memberlist.Address, timeout time.Duration) (net.Conn, error) {
	t.dials.Add(1)
	return t.MockTransport.DialAddressTimeout(addr, timeout)
}

// A link that delivers nothing does not delay probes to a peer the transport
// reaches: probes are answered directly, with no fallback dials, and the peer
// stays a member.
func TestStalledGossipLinkKeepsReachableNodeAlive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	network := &memberlist.MockNetwork{}
	transportA := &dialCountingTransport{MockTransport: network.NewTransport("node-a")}
	transportB := network.NewTransport("node-b")
	addrA, portA, err := transportA.FinalAdvertiseAddr("", 0)
	require.NoError(t, err)

	a := NewService(fastProbeConfig("node-a", transportA, blackholeLink{}, nil), eventbus.NewBus(), zap.NewNop(), nil, nil, nil)
	require.NoError(t, a.Start(ctx))
	t.Cleanup(func() { _ = a.Stop() })
	b := NewService(fastProbeConfig("node-b", transportB, blackholeLink{}, []string{net.JoinHostPort(addrA.String(), strconv.Itoa(portA))}),
		eventbus.NewBus(), zap.NewNop(), nil, nil, nil)
	require.NoError(t, b.Start(ctx))
	t.Cleanup(func() { _ = b.Stop() })

	require.Eventually(t, func() bool { return hasNode(a, "node-b") && hasNode(b, "node-a") }, 5*time.Second, 5*time.Millisecond)
	before := transportA.dials.Load()
	require.Never(t, func() bool { return !hasNode(a, "node-b") || !hasNode(b, "node-a") }, time.Second, 10*time.Millisecond)
	require.Zero(t, transportA.dials.Load()-before, "probes to a reachable peer fell back to TCP")
}
