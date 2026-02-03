// Package testnet provides network simulation for cluster testing.
// It wraps memberlist's MockTransport with traffic control capabilities.
package testnet

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/memberlist"
)

// Network simulates a cluster network with controllable links between nodes.
type Network struct {
	mock   *memberlist.MockNetwork
	nodes  map[string]*Node
	addrs  map[string]string
	links  map[linkKey]*Link
	events []Event
	mu     sync.RWMutex
}

// NewNetwork creates a new simulated network.
func NewNetwork() *Network {
	return &Network{
		mock:  &memberlist.MockNetwork{},
		nodes: make(map[string]*Node),
		addrs: make(map[string]string),
		links: make(map[linkKey]*Link),
	}
}

// AddNode creates a new node in the network.
func (n *Network) AddNode(name string) *Node {
	n.mu.Lock()
	defer n.mu.Unlock()

	transport := n.mock.NewTransport(name)

	// Get the address that MockTransport assigned
	ip, port, _ := transport.FinalAdvertiseAddr("", 0)
	addr := net.JoinHostPort(ip.String(), fmt.Sprintf("%d", port))

	node := &Node{
		name:      name,
		addr:      addr,
		network:   n,
		transport: transport,
		wrapper:   newTransportWrapper(name, addr, transport, n),
	}
	n.nodes[name] = node
	n.addrs[addr] = name

	// Create links to all existing nodes
	for otherName, other := range n.nodes {
		if otherName != name {
			// Bidirectional links
			n.links[linkKey{name, otherName}] = newLink()
			n.links[linkKey{otherName, name}] = newLink()
			_ = other // ensure link exists
		}
	}

	return node
}

// Node returns a node by name.
func (n *Network) Node(name string) *Node {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.nodes[name]
}

// Link returns the link from src to dst (directional).
func (n *Network) Link(src, dst string) *Link {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.links[linkKey{src, dst}]
}

// Partition isolates a node from all others.
func (n *Network) Partition(name string) {
	n.mu.Lock()
	defer n.mu.Unlock()

	for key, link := range n.links {
		if key.from == name || key.to == name {
			link.Block()
		}
	}
	n.recordEvent(EventPartition, name, "")
}

// Heal restores all links for a node.
func (n *Network) Heal(name string) {
	n.mu.Lock()
	defer n.mu.Unlock()

	for key, link := range n.links {
		if key.from == name || key.to == name {
			link.Allow()
		}
	}
	n.recordEvent(EventHeal, name, "")
}

// PartitionBidirectional blocks traffic between two nodes in both directions.
func (n *Network) PartitionBidirectional(a, b string) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if link := n.links[linkKey{a, b}]; link != nil {
		link.Block()
	}
	if link := n.links[linkKey{b, a}]; link != nil {
		link.Block()
	}
	n.recordEvent(EventPartition, a, b)
}

// PartitionAsymmetric blocks traffic from src to dst only.
func (n *Network) PartitionAsymmetric(src, dst string) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if link := n.links[linkKey{src, dst}]; link != nil {
		link.Block()
	}
	n.recordEvent(EventAsymmetricPartition, src, dst)
}

// HealBidirectional restores traffic between two nodes.
func (n *Network) HealBidirectional(a, b string) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if link := n.links[linkKey{a, b}]; link != nil {
		link.Allow()
	}
	if link := n.links[linkKey{b, a}]; link != nil {
		link.Allow()
	}
	n.recordEvent(EventHeal, a, b)
}

// SetLatency adds latency to a directional link.
func (n *Network) SetLatency(src, dst string, d time.Duration) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if link := n.links[linkKey{src, dst}]; link != nil {
		link.SetLatency(d)
	}
}

// SetPacketLoss sets packet drop probability (0.0-1.0) on a directional link.
func (n *Network) SetPacketLoss(src, dst string, rate float64) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if link := n.links[linkKey{src, dst}]; link != nil {
		link.SetPacketLoss(rate)
	}
}

// Events returns recorded network events.
func (n *Network) Events() []Event {
	n.mu.RLock()
	defer n.mu.RUnlock()
	events := make([]Event, len(n.events))
	copy(events, n.events)
	return events
}

// ClearEvents clears recorded events.
func (n *Network) ClearEvents() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = nil
}

func (n *Network) recordEvent(typ EventType, node1, node2 string) {
	n.events = append(n.events, Event{
		Type:  typ,
		Node1: node1,
		Node2: node2,
		Time:  time.Now(),
	})
}

// resolveNodeName converts an address to a node name
func (n *Network) resolveNodeName(addr string) string {
	n.mu.RLock()
	defer n.mu.RUnlock()

	// First check if it's a direct node name
	if _, ok := n.nodes[addr]; ok {
		return addr
	}
	// Otherwise look up by address
	if name, ok := n.addrs[addr]; ok {
		return name
	}
	return addr
}

func (n *Network) shouldDeliver(from, to string) bool {
	fromName := n.resolveNodeName(from)
	toName := n.resolveNodeName(to)

	n.mu.RLock()
	defer n.mu.RUnlock()

	link := n.links[linkKey{fromName, toName}]
	if link == nil {
		return true // no link config = allow
	}
	return link.ShouldDeliver()
}

func (n *Network) getLatency(from, to string) time.Duration {
	fromName := n.resolveNodeName(from)
	toName := n.resolveNodeName(to)

	n.mu.RLock()
	defer n.mu.RUnlock()

	link := n.links[linkKey{fromName, toName}]
	if link == nil {
		return 0
	}
	return link.Latency()
}

type linkKey struct {
	from, to string
}

// Node represents a node in the simulated network.
type Node struct {
	network   *Network
	transport *memberlist.MockTransport
	wrapper   *transportWrapper
	name      string
	addr      string
}

// Name returns the node name.
func (n *Node) Name() string {
	return n.name
}

// Addr returns the node's network address (for JoinAddrs).
func (n *Node) Addr() string {
	return n.addr
}

// Transport returns the memberlist transport for this node.
// Use this when creating memberlist config.
func (n *Node) Transport() memberlist.Transport {
	return n.wrapper
}

// Disconnect simulates a crash - blocks all traffic to/from this node.
func (n *Node) Disconnect() {
	n.network.Partition(n.name)
}

// Reconnect restores connectivity.
func (n *Node) Reconnect() {
	n.network.Heal(n.name)
}

// EventType describes what happened in the network.
type EventType int

const (
	EventPartition EventType = iota
	EventAsymmetricPartition
	EventHeal
	EventPacketSent
	EventPacketDropped
	EventStreamOpened
	EventStreamBlocked
)

func (e EventType) String() string {
	switch e {
	case EventPartition:
		return "PARTITION"
	case EventAsymmetricPartition:
		return "ASYMMETRIC_PARTITION"
	case EventHeal:
		return "HEAL"
	case EventPacketSent:
		return "PACKET_SENT"
	case EventPacketDropped:
		return "PACKET_DROPPED"
	case EventStreamOpened:
		return "STREAM_OPENED"
	case EventStreamBlocked:
		return "STREAM_BLOCKED"
	default:
		return "UNKNOWN"
	}
}

// Event records a network event for test assertions.
type Event struct {
	Time  time.Time
	Node1 string
	Node2 string
	Type  EventType
}

// transportWrapper wraps MockTransport with network simulation.
type transportWrapper struct {
	inner   *memberlist.MockTransport
	network *Network
	name    string
	addr    string
}

func newTransportWrapper(name, addr string, inner *memberlist.MockTransport, network *Network) *transportWrapper {
	return &transportWrapper{
		name:    name,
		addr:    addr,
		inner:   inner,
		network: network,
	}
}

func (t *transportWrapper) FinalAdvertiseAddr(ip string, port int) (net.IP, int, error) {
	return t.inner.FinalAdvertiseAddr(ip, port)
}

func (t *transportWrapper) WriteTo(b []byte, addr string) (time.Time, error) {
	// Extract destination from addr (MockTransport uses node name as addr)
	dst := addr

	if !t.network.shouldDeliver(t.name, dst) {
		// Drop the packet silently
		return time.Now(), nil
	}

	latency := t.network.getLatency(t.name, dst)
	if latency > 0 {
		time.Sleep(latency)
	}

	return t.inner.WriteTo(b, addr)
}

func (t *transportWrapper) PacketCh() <-chan *memberlist.Packet {
	return t.inner.PacketCh()
}

func (t *transportWrapper) DialTimeout(addr string, timeout time.Duration) (net.Conn, error) {
	dst := addr

	if !t.network.shouldDeliver(t.name, dst) {
		return nil, &net.OpError{Op: "dial", Err: net.ErrClosed}
	}

	latency := t.network.getLatency(t.name, dst)
	if latency > 0 {
		time.Sleep(latency)
	}

	return t.inner.DialTimeout(addr, timeout)
}

func (t *transportWrapper) StreamCh() <-chan net.Conn {
	return t.inner.StreamCh()
}

func (t *transportWrapper) Shutdown() error {
	return t.inner.Shutdown()
}
