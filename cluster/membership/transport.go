// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"sync"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/wippyai/runtime/cluster/internode"
)

// GossipLink carries a copy of memberlist packets over connected internode
// links, so a node that can be reached in only one direction still receives
// probes. The memberlist transport carries every packet as well, so a busy or
// reconnecting link never delays a peer that the transport reaches.
type GossipLink interface {
	// SendConnected queues data for node on its connected link and reports
	// whether it did.
	SendConnected(node string, data []byte, class internode.Class) bool
	// RegisterClassReceiver routes inbound frames of class to recv; nil
	// clears the route.
	RegisterClassReceiver(class internode.Class, recv func(node string, data []byte)) bool
}

// linkPacketBuffer bounds packets that arrived on links and wait for
// memberlist. Gossip is lossy, so a full buffer drops the newest packet
// instead of stalling the link's reader, which also carries reliable classes.
const linkPacketBuffer = 256

// linkTransport sends packets over the inner transport and, for a node with a
// connected link, over that link too. Packets that arrive on links join the
// packet stream. memberlist tolerates the duplicates: an ack completes a probe
// once, and broadcasts are idempotent.
type linkTransport struct {
	memberlist.NodeAwareTransport
	link    GossipLink
	packets chan *memberlist.Packet
	done    chan struct{}
	stop    sync.Once
}

func newLinkTransport(inner memberlist.NodeAwareTransport, link GossipLink) *linkTransport {
	t := &linkTransport{
		NodeAwareTransport: inner,
		link:               link,
		packets:            make(chan *memberlist.Packet, linkPacketBuffer),
		done:               make(chan struct{}),
	}
	go t.forward(inner.PacketCh())
	return t
}

// Shutdown keeps forwarding while the inner transport shuts down, because its
// listeners can be blocked handing over a packet, as memberlist's own reader
// keeps reading until the transport is down.
func (t *linkTransport) Shutdown() error {
	err := t.NodeAwareTransport.Shutdown()
	t.release()
	return err
}

// release stops forwarding without shutting the inner transport down.
func (t *linkTransport) release() {
	t.stop.Do(func() { close(t.done) })
}

func (t *linkTransport) WriteTo(b []byte, addr string) (time.Time, error) {
	return t.WriteToAddress(b, memberlist.Address{Addr: addr})
}

// WriteToAddress copies b for the link because memberlist reuses it once the
// call returns and the link sends asynchronously. A packet the link accepted is
// sent even when the inner transport fails.
func (t *linkTransport) WriteToAddress(b []byte, addr memberlist.Address) (time.Time, error) {
	linked := addr.Name != "" && t.link.SendConnected(addr.Name, append([]byte(nil), b...), internode.ClassGossip)
	sent, err := t.NodeAwareTransport.WriteToAddress(b, addr)
	if err != nil && linked {
		return time.Now(), nil
	}
	return sent, err
}

func (t *linkTransport) PacketCh() <-chan *memberlist.Packet {
	return t.packets
}

func (t *linkTransport) forward(in <-chan *memberlist.Packet) {
	for {
		select {
		case <-t.done:
			return
		case packet := <-in:
			select {
			case t.packets <- packet:
			case <-t.done:
				return
			}
		}
	}
}

// deliver hands memberlist a packet node sent over its link.
func (t *linkTransport) deliver(node string, data []byte) {
	select {
	case t.packets <- &memberlist.Packet{Buf: data, From: linkAddr(node), Timestamp: time.Now()}:
	default:
	}
}

// linkAddr identifies the node whose link delivered a packet. Replies to it
// are addressed by node name, which routes them back over the link.
type linkAddr string

func (a linkAddr) Network() string { return "internode" }
func (a linkAddr) String() string  { return string(a) }

// cancelableTransport binds a memberlist transport to a context so shutdown
// reaches sockets that are already in flight. memberlist bounds a dial and the
// push/pull exchange that follows it with Config.TCPTimeout alone, so one
// unreachable or unanswering seed holds a join attempt for that whole timeout
// and the service cannot wait for its join worker without inheriting the stall.
type cancelableTransport struct {
	ctx context.Context
	memberlist.NodeAwareTransport
}

// newCancelableTransport wraps inner, adapting a plain Transport to the
// address-aware interface the same way memberlist does internally.
func newCancelableTransport(ctx context.Context, inner memberlist.Transport) memberlist.NodeAwareTransport {
	aware, ok := inner.(memberlist.NodeAwareTransport)
	if !ok {
		aware = &addressAwareTransport{Transport: inner}
	}
	return &cancelableTransport{ctx: ctx, NodeAwareTransport: aware}
}

func (t *cancelableTransport) DialTimeout(addr string, timeout time.Duration) (net.Conn, error) {
	return t.DialAddressTimeout(memberlist.Address{Addr: addr}, timeout)
}

func (t *cancelableTransport) DialAddressTimeout(addr memberlist.Address, timeout time.Duration) (net.Conn, error) {
	if err := t.ctx.Err(); err != nil {
		return nil, err
	}

	type dialed struct {
		conn net.Conn
		err  error
	}
	// The wrapped transport owns the dial and no context reaches it, so the
	// dial runs alongside the cancellation watch. It is bounded by timeout,
	// and a connection that lands after cancellation is closed rather than
	// handed to memberlist.
	result := make(chan dialed, 1)
	go func() {
		conn, err := t.NodeAwareTransport.DialAddressTimeout(addr, timeout)
		result <- dialed{conn: conn, err: err}
	}()

	select {
	case <-t.ctx.Done():
		go func() {
			if late := <-result; late.conn != nil {
				_ = late.conn.Close()
			}
		}()
		return nil, t.ctx.Err()
	case d := <-result:
		if d.err != nil {
			return nil, d.err
		}
		return newCancelableConn(t.ctx, d.conn), nil
	}
}

// addressAwareTransport lifts a plain Transport to NodeAwareTransport so every
// transport can be wrapped uniformly.
type addressAwareTransport struct {
	memberlist.Transport
}

func (t *addressAwareTransport) WriteToAddress(b []byte, addr memberlist.Address) (time.Time, error) {
	return t.WriteTo(b, addr.Addr)
}

func (t *addressAwareTransport) DialAddressTimeout(addr memberlist.Address, timeout time.Duration) (net.Conn, error) {
	return t.DialTimeout(addr.Addr, timeout)
}

// cancelableConn closes the connection when the service context is cancelled,
// which fails the read and write deadlines memberlist sets from TCPTimeout
// immediately instead of waiting them out.
type cancelableConn struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
}

func newCancelableConn(ctx context.Context, conn net.Conn) net.Conn {
	c := &cancelableConn{Conn: conn, closed: make(chan struct{})}
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Conn.Close()
		case <-c.closed:
		}
	}()
	return c
}

func (c *cancelableConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

// newTransport binds inner to ctx and, when a link is present, routes packets
// over connected links. It returns the link transport so the caller can
// register its receiver.
func newTransport(ctx context.Context, inner memberlist.Transport, link GossipLink) (memberlist.NodeAwareTransport, *linkTransport) {
	cancelable := newCancelableTransport(ctx, inner)
	if link == nil {
		return cancelable, nil
	}
	linked := newLinkTransport(cancelable, link)
	return linked, linked
}

func createMemberlist(ctx context.Context, cfg *memberlist.Config, link GossipLink, open func(*memberlist.NetTransportConfig) (*memberlist.NetTransport, error)) (*memberlist.Memberlist, *linkTransport, error) {
	if cfg.Transport != nil {
		var linked *linkTransport
		cfg.Transport, linked = newTransport(ctx, cfg.Transport, link)
		ml, err := memberlist.Create(cfg)
		if err != nil && linked != nil {
			// The caller owns a supplied transport; only forwarding stops.
			linked.release()
		}
		return ml, linked, err
	}
	logger := cfg.Logger
	if logger == nil {
		output := cfg.LogOutput
		if output == nil {
			output = os.Stderr
		}
		logger = log.New(output, "", log.LstdFlags)
	}
	transportConfig := memberlist.NetTransportConfig{
		BindAddrs: []string{cfg.BindAddr}, BindPort: cfg.BindPort,
		Logger: logger, MetricLabels: cfg.MetricLabels,
	}
	// An explicit port has one candidate, so the automatic allocator below is
	// skipped; membership still owns the transport so shutdown can cancel it.
	if cfg.BindPort != 0 {
		transport, err := open(&transportConfig)
		if err != nil {
			return nil, nil, err
		}
		return createWithTransport(ctx, cfg, link, transport)
	}
	var lastErr error
	for attempt := 0; attempt < 32; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if attempt != 0 {
			// TCP's ephemeral allocator can walk a range unavailable to UDP
			// on Windows. Bind both protocols on a fresh candidate instead.
			candidate, err := rand.Int(rand.Reader, big.NewInt(16384))
			if err != nil {
				return nil, nil, fmt.Errorf("choose membership port: %w", err)
			}
			transportConfig.BindPort = 49152 + int(candidate.Int64())
		}
		transport, err := open(&transportConfig)
		if err != nil {
			lastErr = err
			continue
		}
		if err := ctx.Err(); err != nil {
			_ = transport.Shutdown()
			return nil, nil, err
		}
		cfg.BindPort = transport.GetAutoBindPort()
		cfg.AdvertisePort = cfg.BindPort
		return createWithTransport(ctx, cfg, link, transport)
	}
	return nil, nil, fmt.Errorf("could not allocate membership TCP/UDP listeners after 32 attempts: %w", lastErr)
}

func createWithTransport(ctx context.Context, cfg *memberlist.Config, link GossipLink, transport *memberlist.NetTransport) (*memberlist.Memberlist, *linkTransport, error) {
	var linked *linkTransport
	cfg.Transport, linked = newTransport(ctx, transport, link)
	ml, err := memberlist.Create(cfg)
	if err != nil {
		_ = cfg.Transport.Shutdown()
	}
	return ml, linked, err
}
