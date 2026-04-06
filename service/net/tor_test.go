// SPDX-License-Identifier: MPL-2.0

package net

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	netapi "github.com/wippyai/runtime/api/net"
)

// --- Mock SOCKS5 Server (RFC 1928) ---

// socks5Conn records a single connection attempt through the mock SOCKS5 proxy.
type socks5Conn struct {
	User     string
	Password string
	DestAddr string
	DestPort uint16
}

// mockSOCKS5Server is a minimal SOCKS5 server for testing.
// It records connection attempts and optionally forwards to a backend.
type mockSOCKS5Server struct {
	listener net.Listener
	conns    []socks5Conn
	mu       sync.Mutex

	// backend is dialed for each successful SOCKS5 CONNECT.
	// If nil, the server returns success but immediately closes.
	backend string

	// failConnect makes the server return a SOCKS5 error on CONNECT.
	failConnect bool
}

func newMockSOCKS5Server(t *testing.T) *mockSOCKS5Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	s := &mockSOCKS5Server{listener: ln}
	go s.serve(t)
	return s
}

func (s *mockSOCKS5Server) addr() string {
	return s.listener.Addr().String()
}

func (s *mockSOCKS5Server) close() {
	s.listener.Close()
}

func (s *mockSOCKS5Server) connections() []socks5Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]socks5Conn, len(s.conns))
	copy(cp, s.conns)
	return cp
}

func (s *mockSOCKS5Server) recordConn(c socks5Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns = append(s.conns, c)
}

func (s *mockSOCKS5Server) serve(t *testing.T) {
	t.Helper()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return // listener closed
		}
		go s.handleConn(t, conn)
	}
}

func (s *mockSOCKS5Server) handleConn(t *testing.T, conn net.Conn) {
	t.Helper()
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck

	r := bufio.NewReader(conn)

	// --- Phase 1: Method negotiation ---
	// Client sends: VER(1) NMETHODS(1) METHODS(NMETHODS)
	ver, err := r.ReadByte()
	if err != nil || ver != 0x05 {
		return
	}
	nMethods, err := r.ReadByte()
	if err != nil {
		return
	}
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(r, methods); err != nil {
		return
	}

	// Check if client offers username/password auth (0x02) or no auth (0x00)
	hasUserPass := false
	hasNoAuth := false
	for _, m := range methods {
		if m == 0x02 {
			hasUserPass = true
		}
		if m == 0x00 {
			hasNoAuth = true
		}
	}

	var user, pass string
	if hasUserPass {
		// Select username/password auth
		conn.Write([]byte{0x05, 0x02}) //nolint:errcheck

		// --- Phase 2: Username/Password sub-negotiation (RFC 1929) ---
		subVer, err := r.ReadByte()
		if err != nil || subVer != 0x01 {
			return
		}
		uLen, err := r.ReadByte()
		if err != nil {
			return
		}
		uBuf := make([]byte, uLen)
		if _, err := io.ReadFull(r, uBuf); err != nil {
			return
		}
		pLen, err := r.ReadByte()
		if err != nil {
			return
		}
		pBuf := make([]byte, pLen)
		if _, err := io.ReadFull(r, pBuf); err != nil {
			return
		}
		user = string(uBuf)
		pass = string(pBuf)
		// Accept all credentials
		conn.Write([]byte{0x01, 0x00}) //nolint:errcheck
	} else if hasNoAuth {
		conn.Write([]byte{0x05, 0x00}) //nolint:errcheck
	} else {
		conn.Write([]byte{0x05, 0xFF}) //nolint:errcheck
		return
	}

	// --- Phase 3: CONNECT request ---
	// VER(1) CMD(1) RSV(1) ATYP(1) DST.ADDR(variable) DST.PORT(2)
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return
	}
	if header[0] != 0x05 || header[1] != 0x01 { // VER=5, CMD=CONNECT
		return
	}

	var destAddr string
	switch header[3] { // ATYP
	case 0x01: // IPv4
		ipBuf := make([]byte, 4)
		if _, err := io.ReadFull(r, ipBuf); err != nil {
			return
		}
		destAddr = net.IP(ipBuf).String()
	case 0x03: // Domain name
		domLen, err := r.ReadByte()
		if err != nil {
			return
		}
		domBuf := make([]byte, domLen)
		if _, err := io.ReadFull(r, domBuf); err != nil {
			return
		}
		destAddr = string(domBuf)
	case 0x04: // IPv6
		ipBuf := make([]byte, 16)
		if _, err := io.ReadFull(r, ipBuf); err != nil {
			return
		}
		destAddr = net.IP(ipBuf).String()
	default:
		return
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(r, portBuf); err != nil {
		return
	}
	destPort := binary.BigEndian.Uint16(portBuf)

	s.recordConn(socks5Conn{
		User:     user,
		Password: pass,
		DestAddr: destAddr,
		DestPort: destPort,
	})

	if s.failConnect {
		// Reply with general failure (0x01)
		reply := []byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
		conn.Write(reply) //nolint:errcheck
		return
	}

	// Reply with success
	reply := []byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	conn.Write(reply) //nolint:errcheck

	// If backend is configured, bridge to it
	if s.backend != "" {
		upstream, err := net.DialTimeout("tcp", s.backend, 3*time.Second)
		if err != nil {
			return
		}
		defer upstream.Close()

		done := make(chan struct{})
		go func() {
			io.Copy(upstream, r) //nolint:errcheck
			close(done)
		}()
		io.Copy(conn, upstream) //nolint:errcheck
		<-done
	}
}

// --- Tor Service Tests ---

func TestNewTorService(t *testing.T) {
	cfg := &netapi.TorConfig{
		NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 9050},
	}
	svc, err := NewTorService(cfg)
	require.NoError(t, err)
	require.NotNil(t, svc)

	// Verify it implements the Service interface
	var _ netapi.Service = svc
}

func TestNewTorService_WithStreamIsolation(t *testing.T) {
	cfg := &netapi.TorConfig{
		NetworkConfig:  netapi.NetworkConfig{Host: "127.0.0.1", Port: 9050},
		IsolateStreams: true,
	}
	svc, err := NewTorService(cfg)
	require.NoError(t, err)
	assert.True(t, svc.isolateStreams)
}

func TestTorService_DialContext(t *testing.T) {
	// Start a local HTTP server as the "destination"
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer backend.Close()
	go func() {
		for {
			c, err := backend.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("hello from backend")) //nolint:errcheck
			c.Close()
		}
	}()

	// Start mock SOCKS5 proxy that forwards to the backend
	proxy := newMockSOCKS5Server(t)
	defer proxy.close()
	proxy.backend = backend.Addr().String()

	// Parse proxy address
	host, portStr, err := net.SplitHostPort(proxy.addr())
	require.NoError(t, err)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := &netapi.TorConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
	}
	svc, err := NewTorService(cfg)
	require.NoError(t, err)

	// Dial through the SOCKS5 proxy to the backend
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "example.onion:80")
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	// Read the response from the backend through the proxy
	buf := make([]byte, 100)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello from backend", string(buf[:n]))

	// Verify the proxy saw the correct destination
	conns := proxy.connections()
	require.Len(t, conns, 1)
	assert.Equal(t, "example.onion", conns[0].DestAddr)
	assert.Equal(t, uint16(80), conns[0].DestPort)
	// No auth when stream isolation is disabled
	assert.Empty(t, conns[0].User)
	assert.Empty(t, conns[0].Password)
}

func TestTorService_DialContext_StreamIsolation(t *testing.T) {
	proxy := newMockSOCKS5Server(t)
	defer proxy.close()

	host, portStr, err := net.SplitHostPort(proxy.addr())
	require.NoError(t, err)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := &netapi.TorConfig{
		NetworkConfig:  netapi.NetworkConfig{Host: host, Port: port},
		IsolateStreams: true,
	}
	svc, err := NewTorService(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Make multiple connections with stream isolation
	const numConns = 5
	for i := 0; i < numConns; i++ {
		conn, err := svc.DialContext(ctx, "tcp", fmt.Sprintf("target%d.onion:443", i))
		if err != nil {
			// Some may fail because mock doesn't have a backend, but the
			// SOCKS5 handshake should complete and be recorded
			continue
		}
		conn.Close()
	}

	conns := proxy.connections()
	require.Len(t, conns, numConns)

	// Each connection should have unique credentials (stream isolation)
	userSet := make(map[string]bool)
	for _, c := range conns {
		assert.NotEmpty(t, c.User, "stream isolation should set username")
		assert.NotEmpty(t, c.Password, "stream isolation should set password")
		assert.Equal(t, c.User, c.Password, "Tor isolation uses user==password")
		assert.Len(t, c.User, 16, "credential should be 8 random bytes hex-encoded")
		userSet[c.User] = true
	}
	// All credentials should be unique (different circuits)
	assert.Len(t, userSet, numConns, "each connection should have unique credentials for circuit isolation")
}

func TestTorService_DialContext_ProxyDown(t *testing.T) {
	// Use a port where nothing is listening
	cfg := &netapi.TorConfig{
		NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 19999},
	}
	svc, err := NewTorService(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "example.com:80")
	assert.Nil(t, conn)
	require.Error(t, err)
	// The error should be a connection refused or similar
	assert.True(t,
		isConnectionError(err),
		"expected connection error, got: %v", err)
}

func TestTorService_DialContext_ConnectFailed(t *testing.T) {
	proxy := newMockSOCKS5Server(t)
	proxy.failConnect = true
	defer proxy.close()

	host, portStr, err := net.SplitHostPort(proxy.addr())
	require.NoError(t, err)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := &netapi.TorConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
	}
	svc, err := NewTorService(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "blocked.onion:80")
	assert.Nil(t, conn)
	require.Error(t, err, "should fail when SOCKS5 returns error")
}

func TestTorService_Listen_NotSupported(t *testing.T) {
	cfg := &netapi.TorConfig{
		NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 9050},
	}
	svc, err := NewTorService(cfg)
	require.NoError(t, err)

	ln, err := svc.Listen(context.Background(), "tcp", "0.0.0.0:8080")
	assert.Nil(t, ln)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrNotSupported)
}

func TestTorService_ListenPacket_NotSupported(t *testing.T) {
	cfg := &netapi.TorConfig{
		NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 9050},
	}
	svc, err := NewTorService(cfg)
	require.NoError(t, err)

	pc, err := svc.ListenPacket(context.Background(), "udp", "0.0.0.0:8080")
	assert.Nil(t, pc)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrNotSupported)
}

func TestTorService_LookupHost_NotSupported(t *testing.T) {
	cfg := &netapi.TorConfig{
		NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 9050},
	}
	svc, err := NewTorService(cfg)
	require.NoError(t, err)

	hosts, err := svc.LookupHost(context.Background(), "example.com")
	assert.Nil(t, hosts)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrNotSupported)
}

func TestTorService_DialContext_DomainResolution(t *testing.T) {
	// Verify that .onion domains are passed as-is to the SOCKS5 proxy
	// (not resolved locally, since that would fail for .onion addresses)
	proxy := newMockSOCKS5Server(t)
	defer proxy.close()

	host, portStr, err := net.SplitHostPort(proxy.addr())
	require.NoError(t, err)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := &netapi.TorConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
	}
	svc, err := NewTorService(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test various address formats
	addresses := []struct {
		addr     string
		wantHost string
		wantPort uint16
	}{
		{"duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad.onion:443", "duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad.onion", 443},
		{"example.com:80", "example.com", 80},
		{"10.0.0.1:8080", "10.0.0.1", 8080},
	}

	for _, tc := range addresses {
		conn, err := svc.DialContext(ctx, "tcp", tc.addr)
		if conn != nil {
			conn.Close()
		}
		// Connection might not complete (no backend), but SOCKS5 handshake should succeed
		_ = err
	}

	conns := proxy.connections()
	require.Len(t, conns, len(addresses))

	for i, tc := range addresses {
		assert.Equal(t, tc.wantHost, conns[i].DestAddr, "address %d", i)
		assert.Equal(t, tc.wantPort, conns[i].DestPort, "port %d", i)
	}
}

func TestRandomIsolationCredential(t *testing.T) {
	// Verify credential format
	cred, err := randomIsolationCredential()
	require.NoError(t, err)
	require.NotNil(t, cred)
	assert.Len(t, cred.User, 16, "8 bytes hex-encoded = 16 chars")
	assert.Equal(t, cred.User, cred.Password, "Tor uses same value for user and password")

	// Verify uniqueness
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		c, err := randomIsolationCredential()
		require.NoError(t, err)
		assert.False(t, seen[c.User], "credential collision at iteration %d", i)
		seen[c.User] = true
	}
}

func TestTorConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     netapi.TorConfig
		wantErr string
	}{
		{
			name:    "empty host",
			cfg:     netapi.TorConfig{NetworkConfig: netapi.NetworkConfig{Host: "", Port: 9050}},
			wantErr: "host is required",
		},
		{
			name:    "zero port",
			cfg:     netapi.TorConfig{NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 0}},
			wantErr: "invalid port",
		},
		{
			name:    "negative port",
			cfg:     netapi.TorConfig{NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: -1}},
			wantErr: "invalid port",
		},
		{
			name:    "port too large",
			cfg:     netapi.TorConfig{NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 70000}},
			wantErr: "invalid port",
		},
		{
			name: "valid config",
			cfg:  netapi.TorConfig{NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 9050}},
		},
		{
			name: "valid with stream isolation",
			cfg:  netapi.TorConfig{NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 9050}, IsolateStreams: true},
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

func TestTorService_ConcurrentDial(t *testing.T) {
	proxy := newMockSOCKS5Server(t)
	defer proxy.close()

	host, portStr, err := net.SplitHostPort(proxy.addr())
	require.NoError(t, err)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := &netapi.TorConfig{
		NetworkConfig:  netapi.NetworkConfig{Host: host, Port: port},
		IsolateStreams: true,
	}
	svc, err := NewTorService(cfg)
	require.NoError(t, err)

	const numGoroutines = 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	errs := make([]string, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			conn, err := svc.DialContext(ctx, "tcp", fmt.Sprintf("target%d.onion:443", idx))
			if err != nil {
				errs[idx] = err.Error()
				return
			}
			conn.Close()
		}(i)
	}

	wg.Wait()

	// Verify the proxy recorded all connections
	conns := proxy.connections()
	assert.Len(t, conns, numGoroutines,
		"all concurrent dials should reach the SOCKS5 proxy")

	// All credentials should be unique (stream isolation)
	userSet := make(map[string]bool)
	for _, c := range conns {
		assert.NotEmpty(t, c.User)
		userSet[c.User] = true
	}
	assert.Len(t, userSet, numGoroutines,
		"concurrent dials with stream isolation should produce unique credentials")
}

// isConnectionError checks if the error indicates a network connection failure
// (connection refused, timeout, etc.), including errors wrapped by the proxy package.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	// The SOCKS5 proxy wraps connection errors in its own message
	errStr := err.Error()
	return errors.Is(err, context.DeadlineExceeded) ||
		contains(errStr, "connection refused") ||
		contains(errStr, "connect:") ||
		contains(errStr, "socks connect")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
