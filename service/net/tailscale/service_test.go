// SPDX-License-Identifier: MPL-2.0

package tailscale

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	netapi "github.com/wippyai/runtime/api/net"
)

// --- Mock tsnet node ---

// mockTsnetNode implements the tsnetNode interface for testing.
type mockTsnetNode struct {
	// Dial tracking
	dialFunc func(ctx context.Context, network, address string) (net.Conn, error)

	// Listen tracking
	listenFunc func(network, address string) (net.Listener, error)

	// ListenTLS tracking (Tailscale-issued LetsEncrypt auto-TLS)
	listenTLSFunc func(network, address string) (net.Listener, error)

	// Close tracking
	closeFunc func() error

	dialCalls      []dialCall
	listenCalls    []listenCall
	listenTLSCalls []listenCall

	mu          sync.Mutex
	closeCalled atomic.Int32
}

type dialCall struct {
	Network string
	Address string
}

type listenCall struct {
	Network string
	Address string
}

func newMockTsnetNode() *mockTsnetNode {
	return &mockTsnetNode{}
}

func (m *mockTsnetNode) Dial(ctx context.Context, network, address string) (net.Conn, error) {
	m.mu.Lock()
	m.dialCalls = append(m.dialCalls, dialCall{Network: network, Address: address})
	fn := m.dialFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, network, address)
	}
	return nil, fmt.Errorf("mock: no dial handler configured")
}

func (m *mockTsnetNode) Listen(network, address string) (net.Listener, error) {
	m.mu.Lock()
	m.listenCalls = append(m.listenCalls, listenCall{Network: network, Address: address})
	fn := m.listenFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(network, address)
	}
	return nil, fmt.Errorf("mock: no listen handler configured")
}

func (m *mockTsnetNode) ListenTLS(network, address string) (net.Listener, error) {
	m.mu.Lock()
	m.listenTLSCalls = append(m.listenTLSCalls, listenCall{Network: network, Address: address})
	fn := m.listenTLSFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(network, address)
	}
	return nil, fmt.Errorf("mock: no listenTLS handler configured")
}

func (m *mockTsnetNode) Close() error {
	m.closeCalled.Add(1)
	m.mu.Lock()
	fn := m.closeFunc
	m.mu.Unlock()
	if fn != nil {
		return fn()
	}
	return nil
}

func (m *mockTsnetNode) getDialCalls() []dialCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]dialCall, len(m.dialCalls))
	copy(cp, m.dialCalls)
	return cp
}

func (m *mockTsnetNode) getListenCalls() []listenCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]listenCall, len(m.listenCalls))
	copy(cp, m.listenCalls)
	return cp
}

// --- Service Unit Tests ---

func TestTailscaleService_ImplementsInterface(t *testing.T) {
	mock := newMockTsnetNode()
	svc := newServiceWithNode(mock)
	var _ netapi.Service = svc
}

func TestTailscaleService_DialContext(t *testing.T) {
	// Start a local backend to simulate a tailnet peer
	backend, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer backend.Close()
	go func() {
		for {
			c, err := backend.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("hello from tailscale peer"))
			c.Close()
		}
	}()

	mock := newMockTsnetNode()
	mock.dialFunc = func(_ context.Context, network, address string) (net.Conn, error) {
		// Simulate tsnet routing by connecting to the local backend
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(context.Background(), network, backend.Addr().String())
	}

	svc := newServiceWithNode(mock)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "my-peer.tailnet:443")
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	// Read the response from the backend through the "tailnet"
	buf := make([]byte, 100)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello from tailscale peer", string(buf[:n]))

	// Verify the mock saw the correct destination
	calls := mock.getDialCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "tcp", calls[0].Network)
	assert.Equal(t, "my-peer.tailnet:443", calls[0].Address)
}

func TestTailscaleService_DialContext_VariousAddresses(t *testing.T) {
	mock := newMockTsnetNode()
	mock.dialFunc = func(_ context.Context, _, _ string) (net.Conn, error) {
		// Return a pipe so the connection "succeeds"
		c1, _ := net.Pipe()
		return c1, nil
	}

	svc := newServiceWithNode(mock)
	ctx := context.Background()

	addresses := []struct {
		network string
		address string
	}{
		{"tcp", "server.tailnet:80"},
		{"tcp", "192.168.1.100:22"},
		{"tcp", "node-a.example.ts.net:8080"},
		{"tcp4", "10.0.0.5:3000"},
	}

	for _, tc := range addresses {
		conn, err := svc.DialContext(ctx, tc.network, tc.address)
		if conn != nil {
			conn.Close()
		}
		_ = err
	}

	calls := mock.getDialCalls()
	require.Len(t, calls, len(addresses))

	for i, tc := range addresses {
		assert.Equal(t, tc.network, calls[i].Network, "call %d network", i)
		assert.Equal(t, tc.address, calls[i].Address, "call %d address", i)
	}
}

func TestTailscaleService_DialContext_NodeDown(t *testing.T) {
	mock := newMockTsnetNode()
	mock.dialFunc = func(_ context.Context, _, _ string) (net.Conn, error) {
		return nil, fmt.Errorf("tailscale: node is offline")
	}

	svc := newServiceWithNode(mock)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "unreachable-peer.tailnet:443")
	assert.Nil(t, conn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "node is offline")
}

func TestTailscaleService_DialContext_ContextCancelled(t *testing.T) {
	mock := newMockTsnetNode()
	mock.dialFunc = func(ctx context.Context, _, _ string) (net.Conn, error) {
		// Simulate slow dial that respects context
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Second):
			c1, _ := net.Pipe()
			return c1, nil
		}
	}

	svc := newServiceWithNode(mock)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "slow-peer.tailnet:80")
	assert.Nil(t, conn)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestTailscaleService_DialContext_DataTransfer(t *testing.T) {
	// Test bidirectional data transfer through the mock tailnet
	mock := newMockTsnetNode()
	mock.dialFunc = func(_ context.Context, _, _ string) (net.Conn, error) {
		c1, c2 := net.Pipe()
		// Simulate a peer that echoes data back
		go func() {
			defer c2.Close()
			buf := make([]byte, 1024)
			for {
				n, err := c2.Read(buf)
				if err != nil {
					return
				}
				c2.Write(buf[:n])
			}
		}()
		return c1, nil
	}

	svc := newServiceWithNode(mock)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "echo-peer.tailnet:7")
	require.NoError(t, err)
	defer conn.Close()

	// Send data and verify echo
	testData := "hello tailscale network"
	_, err = conn.Write([]byte(testData))
	require.NoError(t, err)

	buf := make([]byte, 100)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, testData, string(buf[:n]))
}

func TestTailscaleService_Listen(t *testing.T) {
	// Start a real listener to return from the mock
	realLn, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer realLn.Close()

	mock := newMockTsnetNode()
	mock.listenFunc = func(network, address string) (net.Listener, error) {
		return realLn, nil
	}

	svc := newServiceWithNode(mock)

	ln, err := svc.Listen(context.Background(), "tcp", "0.0.0.0:8080")
	require.NoError(t, err)
	require.NotNil(t, ln)

	calls := mock.getListenCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "tcp", calls[0].Network)
	assert.Equal(t, "0.0.0.0:8080", calls[0].Address)
}

func TestTailscaleService_Listen_Error(t *testing.T) {
	mock := newMockTsnetNode()
	mock.listenFunc = func(_, _ string) (net.Listener, error) {
		return nil, fmt.Errorf("tailscale: port already in use")
	}

	svc := newServiceWithNode(mock)

	ln, err := svc.Listen(context.Background(), "tcp", "0.0.0.0:80")
	assert.Nil(t, ln)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port already in use")
}

func TestTailscaleService_Listen_AcceptConnections(t *testing.T) {
	// Verify that a listener returned by Service.Listen can accept connections
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	mock := newMockTsnetNode()
	mock.listenFunc = func(_, _ string) (net.Listener, error) {
		return ln, nil
	}

	svc := newServiceWithNode(mock)

	listener, err := svc.Listen(context.Background(), "tcp", "0.0.0.0:9090")
	require.NoError(t, err)

	// Accept in background
	done := make(chan net.Conn, 1)
	go func() {
		c, err := listener.Accept()
		if err != nil {
			return
		}
		done <- c
	}()

	// Connect to the listener
	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(context.Background(), "tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	select {
	case accepted := <-done:
		require.NotNil(t, accepted)
		accepted.Close()
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for Accept")
	}
}

func TestTailscaleService_ListenPacket_NotSupported(t *testing.T) {
	mock := newMockTsnetNode()
	svc := newServiceWithNode(mock)

	pc, err := svc.ListenPacket(context.Background(), "udp", "0.0.0.0:8080")
	assert.Nil(t, pc)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrNotSupported)
}

func TestTailscaleService_LookupHost_NotSupported(t *testing.T) {
	mock := newMockTsnetNode()
	svc := newServiceWithNode(mock)

	hosts, err := svc.LookupHost(context.Background(), "peer.tailnet")
	assert.Nil(t, hosts)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrNotSupported)
}

func TestTailscaleService_Close(t *testing.T) {
	mock := newMockTsnetNode()
	svc := newServiceWithNode(mock)

	err := svc.Close()
	assert.NoError(t, err)
	assert.Equal(t, int32(1), mock.closeCalled.Load(), "Close should be called exactly once")
}

func TestTailscaleService_Close_Error(t *testing.T) {
	mock := newMockTsnetNode()
	mock.closeFunc = func() error {
		return fmt.Errorf("tailscale: shutdown timeout")
	}

	svc := newServiceWithNode(mock)

	err := svc.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shutdown timeout")
}

func TestTailscaleService_Close_Idempotent(t *testing.T) {
	mock := newMockTsnetNode()
	svc := newServiceWithNode(mock)

	// Close multiple times should not panic
	for i := 0; i < 3; i++ {
		err := svc.Close()
		assert.NoError(t, err)
	}
	assert.Equal(t, int32(3), mock.closeCalled.Load())
}

func TestTailscaleService_ConcurrentDial(t *testing.T) {
	mock := newMockTsnetNode()
	mock.dialFunc = func(_ context.Context, _, _ string) (net.Conn, error) {
		c1, c2 := net.Pipe()
		go func() {
			io.Copy(io.Discard, c2)
			c2.Close()
		}()
		return c1, nil
	}

	svc := newServiceWithNode(mock)

	const numGoroutines = 20
	var wg sync.WaitGroup
	errs := make(chan error, numGoroutines)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			conn, err := svc.DialContext(ctx, "tcp", fmt.Sprintf("peer%d.tailnet:80", idx))
			if err != nil {
				errs <- err
				return
			}
			conn.Close()
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent dial failed: %v", err)
	}

	calls := mock.getDialCalls()
	assert.Len(t, calls, numGoroutines, "all dials should be recorded")
}

func TestTailscaleService_ConcurrentListen(t *testing.T) {
	mock := newMockTsnetNode()
	var listenerCount atomic.Int32
	mock.listenFunc = func(_, _ string) (net.Listener, error) {
		ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		listenerCount.Add(1)
		return ln, nil
	}

	svc := newServiceWithNode(mock)

	const numGoroutines = 10
	var wg sync.WaitGroup
	listeners := make(chan net.Listener, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ln, err := svc.Listen(context.Background(), "tcp", fmt.Sprintf("0.0.0.0:%d", 8000+idx))
			if err != nil {
				return
			}
			listeners <- ln
		}(i)
	}

	wg.Wait()
	close(listeners)

	for ln := range listeners {
		ln.Close()
	}

	assert.Equal(t, int32(numGoroutines), listenerCount.Load())
}

func TestTailscaleService_DialAndClose(t *testing.T) {
	// Test that closing the service while a dial is in progress doesn't panic
	mock := newMockTsnetNode()
	dialStarted := make(chan struct{})
	mock.dialFunc = func(ctx context.Context, _, _ string) (net.Conn, error) {
		close(dialStarted)
		// Block until context cancelled
		<-ctx.Done()
		return nil, ctx.Err()
	}

	svc := newServiceWithNode(mock)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start a dial in background
	dialDone := make(chan error, 1)
	go func() {
		_, err := svc.DialContext(ctx, "tcp", "peer.tailnet:80")
		dialDone <- err
	}()

	// Wait for dial to start, then close
	<-dialStarted
	err := svc.Close()
	assert.NoError(t, err)

	// Cancel context to unblock the dial
	cancel()
	dialErr := <-dialDone
	require.Error(t, dialErr)
}

// --- Config Validation Tests ---

func TestTailscaleConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		wantErr string
		cfg     netapi.TailscaleConfig
	}{
		{
			name:    "no auth key",
			cfg:     netapi.TailscaleConfig{},
			wantErr: "auth_key",
		},
		{
			name: "auth_key set",
			cfg:  netapi.TailscaleConfig{AuthKey: "tskey-auth-xxx"},
		},
		{
			name: "auth_key_env set",
			cfg:  netapi.TailscaleConfig{AuthKeyEnv: "TS_AUTHKEY"},
		},
		{
			name: "both set",
			cfg:  netapi.TailscaleConfig{AuthKey: "tskey-auth-xxx", AuthKeyEnv: "TS_AUTHKEY"},
		},
		{
			name: "with hostname",
			cfg:  netapi.TailscaleConfig{AuthKey: "tskey-auth-xxx", Hostname: "my-node"},
		},
		{
			name: "with state dir",
			cfg:  netapi.TailscaleConfig{AuthKey: "tskey-auth-xxx", StateDir: "/tmp/ts-state"},
		},
		{
			name: "with control url",
			cfg:  netapi.TailscaleConfig{AuthKey: "tskey-auth-xxx", ControlURL: "https://headscale.example.com"},
		},
		{
			name: "ephemeral",
			cfg:  netapi.TailscaleConfig{AuthKey: "tskey-auth-xxx", Ephemeral: true},
		},
		{
			name: "full config",
			cfg: netapi.TailscaleConfig{
				AuthKey:    "tskey-auth-xxx",
				Hostname:   "my-node",
				StateDir:   "/tmp/ts-state",
				ControlURL: "https://headscale.example.com",
				Ephemeral:  true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- DNS Resolution Tests ---
// Tailscale resolves DNS within the tailnet. The service should NOT
// perform any local DNS resolution. These tests verify that addresses
// are passed as-is to the tsnet node.

func TestTailscaleService_AddressPassthrough(t *testing.T) {
	// Verify that Tailscale-specific addresses (like .ts.net, MagicDNS names,
	// and tailnet IPs) are passed directly to the tsnet node without
	// local DNS resolution.
	mock := newMockTsnetNode()
	mock.dialFunc = func(_ context.Context, _, _ string) (net.Conn, error) {
		c1, c2 := net.Pipe()
		go c2.Close()
		return c1, nil
	}

	svc := newServiceWithNode(mock)
	ctx := context.Background()

	addresses := []string{
		"my-server.tailnet:22",                  // tailnet hostname
		"my-server.example.ts.net:443",          // MagicDNS FQDN
		"100.64.0.1:80",                         // CGNAT tailscale IP
		"fd7a:115c:a1e0::1:443",                 // tailscale IPv6
		"my-server:8080",                        // short hostname
		"database.internal.example.ts.net:5432", // deep MagicDNS name
	}

	for _, addr := range addresses {
		conn, err := svc.DialContext(ctx, "tcp", addr)
		if conn != nil {
			conn.Close()
		}
		_ = err
	}

	calls := mock.getDialCalls()
	require.Len(t, calls, len(addresses))

	for i, addr := range addresses {
		assert.Equal(t, addr, calls[i].Address,
			"address %q should be passed as-is to tsnet node", addr)
	}
}

func TestTailscaleService_NoDNSLeak(t *testing.T) {
	// Verify that even non-tailscale addresses go through the tsnet node
	// (the node handles all routing, not the local DNS resolver)
	dnsLookupAttempted := atomic.Bool{}

	mock := newMockTsnetNode()
	mock.dialFunc = func(_ context.Context, _, address string) (net.Conn, error) {
		// If we got here, the address was NOT resolved locally first
		// (since local DNS would have replaced the hostname with an IP)
		host, _, _ := net.SplitHostPort(address)
		if ip := net.ParseIP(host); ip != nil && host != "100.64.0.1" {
			// If it's an IP that's not a tailscale CGNAT address, DNS leaked
			dnsLookupAttempted.Store(true)
		}
		return nil, fmt.Errorf("mock: connection refused")
	}

	svc := newServiceWithNode(mock)
	ctx := context.Background()

	// These hostnames should NOT be resolved locally
	hostnames := []string{
		"my-server.tailnet:80",
		"node-a.ts.net:443",
		"internal-service:8080",
	}

	for _, addr := range hostnames {
		conn, _ := svc.DialContext(ctx, "tcp", addr)
		if conn != nil {
			conn.Close()
		}
	}

	assert.False(t, dnsLookupAttempted.Load(),
		"no addresses should be resolved via local DNS")

	// Verify all addresses were passed as hostnames
	calls := mock.getDialCalls()
	for _, call := range calls {
		host, _, _ := net.SplitHostPort(call.Address)
		assert.Nil(t, net.ParseIP(host),
			"address %q should remain as hostname (not resolved to IP)", call.Address)
	}
}

// --- Traffic Routing Verification ---

func TestTailscaleService_AllTrafficGoesThrough_TsnetNode(t *testing.T) {
	// This is the Tailscale equivalent of "does all traffic go through the overlay?"
	// For Tailscale, the answer is yes by construction: Service.DialContext
	// delegates directly to tsnetNode.Dial, and Service.Listen delegates
	// to tsnetNode.Listen. There is no code path that bypasses the node.
	//
	// We verify by:
	// 1. All DialContext calls go through the mock (no direct net.Dial)
	// 2. All Listen calls go through the mock (no direct net.Listen)
	// 3. No DNS resolution happens locally

	var dialCount, listenCount atomic.Int32

	mock := newMockTsnetNode()
	mock.dialFunc = func(_ context.Context, _, _ string) (net.Conn, error) {
		dialCount.Add(1)
		c1, c2 := net.Pipe()
		go c2.Close()
		return c1, nil
	}
	mock.listenFunc = func(_, _ string) (net.Listener, error) {
		listenCount.Add(1)
		return (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	}

	svc := newServiceWithNode(mock)
	ctx := context.Background()

	// Make several dials
	for i := 0; i < 5; i++ {
		conn, err := svc.DialContext(ctx, "tcp", fmt.Sprintf("peer%d.tailnet:80", i))
		if conn != nil {
			conn.Close()
		}
		_ = err
	}

	// Make several listens
	for i := 0; i < 3; i++ {
		ln, err := svc.Listen(ctx, "tcp", fmt.Sprintf("0.0.0.0:%d", 9000+i))
		if ln != nil {
			ln.Close()
		}
		_ = err
	}

	assert.Equal(t, int32(5), dialCount.Load(), "all 5 dials should go through tsnet node")
	assert.Equal(t, int32(3), listenCount.Load(), "all 3 listens should go through tsnet node")
}
