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
)

// candidateTransport leaves memberlist's wire protocol unchanged. For a known
// node, UDP gossip reaches every bounded advertised path and TCP probes race
// those paths. Memberlist still authenticates/decrypts received messages.
type candidateTransport struct {
	memberlist.NodeAwareTransport
	resolve func(string) []string
}

func newCandidateTransport(inner memberlist.NodeAwareTransport, resolve func(string) []string) memberlist.NodeAwareTransport {
	return &candidateTransport{NodeAwareTransport: inner, resolve: resolve}
}

func (t *candidateTransport) addresses(a memberlist.Address) []memberlist.Address {
	if a.Name == "" || t.resolve == nil {
		return []memberlist.Address{a}
	}
	seen := make(map[string]bool)
	var out []memberlist.Address
	for _, addr := range t.resolve(a.Name) {
		if addr == "" || seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, memberlist.Address{Name: a.Name, Addr: addr})
		if len(out) >= 7 {
			break
		}
	}
	if a.Addr != "" && !seen[a.Addr] {
		out = append(out, a)
	}
	return out
}

func (t *candidateTransport) WriteTo(b []byte, addr string) (time.Time, error) {
	return t.WriteToAddress(b, memberlist.Address{Addr: addr})
}

func (t *candidateTransport) WriteToAddress(b []byte, addr memberlist.Address) (time.Time, error) {
	var sent time.Time
	var lastErr error
	succeeded := false
	for _, candidate := range t.addresses(addr) {
		when, err := t.NodeAwareTransport.WriteToAddress(b, candidate)
		if err == nil {
			sent = when
			succeeded = true
		} else {
			lastErr = err
		}
	}
	if succeeded {
		return sent, nil
	}
	return sent, lastErr
}

func (t *candidateTransport) DialTimeout(addr string, timeout time.Duration) (net.Conn, error) {
	return t.DialAddressTimeout(memberlist.Address{Addr: addr}, timeout)
}

func (t *candidateTransport) DialAddressTimeout(addr memberlist.Address, timeout time.Duration) (net.Conn, error) {
	candidates := t.addresses(addr)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no memberlist address")
	}
	if len(candidates) == 1 {
		return t.NodeAwareTransport.DialAddressTimeout(candidates[0], timeout)
	}
	type result struct {
		conn net.Conn
		err  error
	}
	results := make(chan result, len(candidates))
	for i, candidate := range candidates {
		go func(i int, a memberlist.Address) {
			if i > 0 {
				time.Sleep(time.Duration(i) * 20 * time.Millisecond)
			}
			conn, err := t.NodeAwareTransport.DialAddressTimeout(a, timeout)
			results <- result{conn, err}
		}(i, candidate)
	}
	var lastErr error
	for remaining := len(candidates); remaining > 0; remaining-- {
		r := <-results
		if r.err != nil {
			lastErr = r.err
			continue
		}
		go func(n int) {
			for i := 0; i < n; i++ {
				if late := <-results; late.conn != nil {
					_ = late.conn.Close()
				}
			}
		}(remaining - 1)
		return r.conn, nil
	}
	return nil, lastErr
}

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

func createMemberlist(ctx context.Context, cfg *memberlist.Config, open func(*memberlist.NetTransportConfig) (*memberlist.NetTransport, error)) (*memberlist.Memberlist, error) {
	return createMemberlistWithRoutes(ctx, cfg, open, nil)
}

func createMemberlistWithRoutes(ctx context.Context, cfg *memberlist.Config, open func(*memberlist.NetTransportConfig) (*memberlist.NetTransport, error), resolve func(string) []string) (*memberlist.Memberlist, error) {
	if cfg.Transport != nil {
		aware, ok := cfg.Transport.(memberlist.NodeAwareTransport)
		if !ok {
			aware = &addressAwareTransport{Transport: cfg.Transport}
		}
		if resolve != nil {
			aware = newCandidateTransport(aware, resolve)
		}
		cfg.Transport = newCancelableTransport(ctx, aware)
		return memberlist.Create(cfg)
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
			return nil, err
		}
		return createWithTransportRoutes(ctx, cfg, transport, resolve)
	}
	var lastErr error
	for attempt := 0; attempt < 32; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if attempt != 0 {
			// TCP's ephemeral allocator can walk a range unavailable to UDP
			// on Windows. Bind both protocols on a fresh candidate instead.
			candidate, err := rand.Int(rand.Reader, big.NewInt(16384))
			if err != nil {
				return nil, fmt.Errorf("choose membership port: %w", err)
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
			return nil, err
		}
		cfg.BindPort = transport.GetAutoBindPort()
		cfg.AdvertisePort = cfg.BindPort
		return createWithTransportRoutes(ctx, cfg, transport, resolve)
	}
	return nil, fmt.Errorf("could not allocate membership TCP/UDP listeners after 32 attempts: %w", lastErr)
}

func createWithTransport(ctx context.Context, cfg *memberlist.Config, transport *memberlist.NetTransport) (*memberlist.Memberlist, error) {
	return createWithTransportRoutes(ctx, cfg, transport, nil)
}

func createWithTransportRoutes(ctx context.Context, cfg *memberlist.Config, transport *memberlist.NetTransport, resolve func(string) []string) (*memberlist.Memberlist, error) {
	var aware memberlist.NodeAwareTransport = transport
	if resolve != nil {
		aware = newCandidateTransport(aware, resolve)
	}
	cfg.Transport = newCancelableTransport(ctx, aware)
	ml, err := memberlist.Create(cfg)
	if err != nil {
		_ = transport.Shutdown()
	}
	return ml, err
}
