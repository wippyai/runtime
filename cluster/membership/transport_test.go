// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/stretchr/testify/require"
)

func automaticTransportConfig() *memberlist.Config {
	cfg := memberlist.DefaultLocalConfig()
	cfg.Name = "transport-test"
	cfg.BindAddr = "127.0.0.1"
	cfg.BindPort = 0
	cfg.LogOutput = io.Discard
	return cfg
}

func TestAutomaticTransportRetriesAndRetainsBothListeners(t *testing.T) {
	cfg := automaticTransportConfig()
	attempts := 0
	ml, err := createMemberlist(t.Context(), cfg, func(nc *memberlist.NetTransportConfig) (*memberlist.NetTransport, error) {
		attempts++
		if attempts == 1 {
			require.Zero(t, nc.BindPort)
			return nil, errors.New("UDP port excluded")
		}
		require.GreaterOrEqual(t, nc.BindPort, 49152)
		require.LessOrEqual(t, nc.BindPort, 65535)
		return memberlist.NewNetTransport(nc)
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ml.Shutdown() })
	require.GreaterOrEqual(t, attempts, 2)
	require.Equal(t, cfg.BindPort, cfg.AdvertisePort)
	require.Equal(t, cfg.BindPort, int(ml.LocalNode().Port))
	address := net.JoinHostPort(cfg.BindAddr, strconv.Itoa(cfg.BindPort))
	tcp, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", address)
	if tcp != nil {
		_ = tcp.Close()
	}
	require.Error(t, err, "membership must retain its TCP listener")
	udp, err := (&net.ListenConfig{}).ListenPacket(t.Context(), "udp", address)
	if udp != nil {
		_ = udp.Close()
	}
	require.Error(t, err, "membership must retain its UDP listener")
	require.NoError(t, ml.Shutdown())
	assertTransportPortReleased(t, address)
}

func TestAutomaticTransportFailureIsBounded(t *testing.T) {
	cfg := automaticTransportConfig()
	want := errors.New("bind denied")
	attempts := 0
	_, err := createMemberlist(t.Context(), cfg, func(*memberlist.NetTransportConfig) (*memberlist.NetTransport, error) {
		attempts++
		return nil, want
	})
	require.ErrorIs(t, err, want)
	require.Equal(t, 32, attempts)
	require.Nil(t, cfg.Transport)
	require.Zero(t, cfg.BindPort)
}

func TestAutomaticTransportCancellationStopsRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	attempts := 0
	_, err := createMemberlist(ctx, automaticTransportConfig(), func(*memberlist.NetTransportConfig) (*memberlist.NetTransport, error) {
		attempts++
		cancel()
		return nil, errors.New("bind denied")
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, attempts)
}

func TestAutomaticTransportClosesOnMemberlistFailure(t *testing.T) {
	cfg := automaticTransportConfig()
	cfg.AdvertiseAddr = "invalid-ip"
	_, err := createMemberlist(t.Context(), cfg, memberlist.NewNetTransport)
	require.ErrorContains(t, err, "advertise address")
	require.Positive(t, cfg.BindPort)
	assertTransportPortReleased(t, net.JoinHostPort(cfg.BindAddr, strconv.Itoa(cfg.BindPort)))
}

func TestAutomaticTransportClosesWhenCanceledAfterBind(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var address string
	_, err := createMemberlist(ctx, automaticTransportConfig(), func(nc *memberlist.NetTransportConfig) (*memberlist.NetTransport, error) {
		transport, err := memberlist.NewNetTransport(nc)
		if err == nil {
			address = net.JoinHostPort(nc.BindAddrs[0], strconv.Itoa(transport.GetAutoBindPort()))
			cancel()
		}
		return transport, err
	})
	require.ErrorIs(t, err, context.Canceled)
	require.NotEmpty(t, address)
	assertTransportPortReleased(t, address)
}

func TestFixedTransportDoesNotUseAutomaticAllocator(t *testing.T) {
	cfg := automaticTransportConfig()
	cfg.BindPort = -1
	attempts := 0
	_, err := createMemberlist(t.Context(), cfg, func(nc *memberlist.NetTransportConfig) (*memberlist.NetTransport, error) {
		attempts++
		require.Equal(t, -1, nc.BindPort, "explicit port must not use automatic allocator")
		return memberlist.NewNetTransport(nc)
	})
	require.Error(t, err)
	require.Equal(t, 1, attempts, "explicit port must not be retried on a fresh candidate")
	require.Equal(t, -1, cfg.BindPort)
}

func TestFixedTransportIsCancelable(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cfg := automaticTransportConfig()
	port, err := freeLoopbackPort(t)
	require.NoError(t, err)
	cfg.BindPort = port
	ml, err := createMemberlist(ctx, cfg, memberlist.NewNetTransport)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ml.Shutdown() })
	require.IsType(t, &cancelableTransport{}, cfg.Transport)
	cancel()
	_, err = cfg.Transport.DialTimeout(net.JoinHostPort(cfg.BindAddr, strconv.Itoa(port)), time.Minute)
	require.ErrorIs(t, err, context.Canceled)
}

// freeLoopbackPort reserves a port that is free for both UDP and TCP, the two
// protocols memberlist binds. The UDP allocator goes first: it never hands out
// a port from a range the platform excludes for UDP.
func freeLoopbackPort(t *testing.T) (int, error) {
	t.Helper()
	var listenConfig net.ListenConfig
	udp, err := listenConfig.ListenPacket(t.Context(), "udp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := udp.LocalAddr().(*net.UDPAddr).Port
	tcp, err := listenConfig.Listen(t.Context(), "tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return 0, errors.Join(err, udp.Close())
	}
	return port, errors.Join(tcp.Close(), udp.Close())
}

func assertTransportPortReleased(t *testing.T, address string) {
	t.Helper()
	tcp, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", address)
	require.NoError(t, err)
	defer tcp.Close()
	udp, err := (&net.ListenConfig{}).ListenPacket(t.Context(), "udp", address)
	require.NoError(t, err)
	require.NoError(t, udp.Close())
}

// TestCancelableTransportAbortsBlockedDial covers the dial itself, which no
// context reaches: the wrapped transport owns it and only its own timeout ends
// it, so an unreachable seed would otherwise hold the caller for TCPTimeout.
func TestCancelableTransportAbortsBlockedDial(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	inner := &blockingDialTransport{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
		dialed:  make(chan *closeSignalConn, 1),
	}
	transport := newCancelableTransport(ctx, inner)

	dialErr := make(chan error, 1)
	go func() {
		_, err := transport.DialTimeout("127.0.0.1:7946", time.Minute)
		dialErr <- err
	}()
	<-inner.started
	cancel()
	select {
	case err := <-dialErr:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation must abort a dial the transport cannot interrupt")
	}

	close(inner.release)
	conn := <-inner.dialed
	select {
	case <-conn.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("a connection that lands after cancellation must be closed")
	}
}

func TestCancelableTransportClosesConnOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	inner := &blockingDialTransport{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
		dialed:  make(chan *closeSignalConn, 1),
	}
	close(inner.release)
	transport := newCancelableTransport(ctx, inner)

	conn, err := transport.DialTimeout("127.0.0.1:7946", time.Minute)
	require.NoError(t, err)
	cancel()
	select {
	case <-(<-inner.dialed).closed:
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation must close an established connection")
	}
	require.NoError(t, conn.Close())
}

type blockingDialTransport struct {
	memberlist.Transport
	started chan struct{}
	release chan struct{}
	dialed  chan *closeSignalConn
}

func (t *blockingDialTransport) DialTimeout(string, time.Duration) (net.Conn, error) {
	t.started <- struct{}{}
	<-t.release
	local, remote := net.Pipe()
	_ = remote.Close()
	conn := &closeSignalConn{Conn: local, closed: make(chan struct{})}
	t.dialed <- conn
	return conn, nil
}

type closeSignalConn struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
}

func (c *closeSignalConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}
