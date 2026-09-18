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
	if cfg.Transport != nil {
		cfg.Transport = newCancelableTransport(ctx, cfg.Transport)
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
		return createWithTransport(ctx, cfg, transport)
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
		return createWithTransport(ctx, cfg, transport)
	}
	return nil, fmt.Errorf("could not allocate membership TCP/UDP listeners after 32 attempts: %w", lastErr)
}

func createWithTransport(ctx context.Context, cfg *memberlist.Config, transport *memberlist.NetTransport) (*memberlist.Memberlist, error) {
	cfg.Transport = newCancelableTransport(ctx, transport)
	ml, err := memberlist.Create(cfg)
	if err != nil {
		_ = transport.Shutdown()
	}
	return ml, err
}
