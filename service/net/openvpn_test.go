// SPDX-License-Identifier: MPL-2.0

package net

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	netapi "github.com/wippyai/runtime/api/net"
)

// --- Mock OpenVPN Management Server ---

// mockOpenVPNMgmt is a TCP server that speaks the OpenVPN management
// interface protocol. It responds to "state" commands with configurable
// state/localIP and optionally requires a password.
type mockOpenVPNMgmt struct {
	ln       net.Listener
	t        *testing.T
	password string // if set, require password auth
	localIP  string // IP returned in CONNECTED state line
	state    string // e.g. "CONNECTED", "RECONNECTING", "ASSIGN_IP"
	mu       sync.Mutex
	conns    int // number of management connections received
}

func newMockOpenVPNMgmt(t *testing.T, localIP, state string, opts ...func(*mockOpenVPNMgmt)) *mockOpenVPNMgmt {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	m := &mockOpenVPNMgmt{
		ln:      ln,
		t:       t,
		localIP: localIP,
		state:   state,
	}
	for _, opt := range opts {
		opt(m)
	}
	go m.serve()
	return m
}

func withPassword(pw string) func(*mockOpenVPNMgmt) {
	return func(m *mockOpenVPNMgmt) {
		m.password = pw
	}
}

func (m *mockOpenVPNMgmt) addr() string {
	return m.ln.Addr().String()
}

func (m *mockOpenVPNMgmt) close() {
	m.ln.Close()
}

func (m *mockOpenVPNMgmt) serve() {
	for {
		conn, err := m.ln.Accept()
		if err != nil {
			return
		}
		m.mu.Lock()
		m.conns++
		m.mu.Unlock()
		go m.handle(conn)
	}
}

func (m *mockOpenVPNMgmt) handle(conn net.Conn) {
	defer conn.Close()

	// If password is set, prompt for it FIRST (matches real OpenVPN behavior)
	if m.password != "" {
		fmt.Fprintf(conn, "ENTER PASSWORD:\n")
		scanner := bufio.NewScanner(conn)
		if !scanner.Scan() {
			return
		}
		pw := strings.TrimSpace(scanner.Text())
		if pw != m.password {
			fmt.Fprintf(conn, "ERROR: bad password\n")
			return
		}
		fmt.Fprintf(conn, "SUCCESS: password is correct\n")
		// Send banner after successful auth
		fmt.Fprintf(conn, ">INFO:OpenVPN Management Interface Version 5 -- type 'help' for more info\n")

		m.handleCommands(conn, scanner)
		return
	}

	// No password — send banner, then wait for commands
	fmt.Fprintf(conn, ">INFO:OpenVPN Management Interface Version 5 -- type 'help' for more info\n")
	scanner := bufio.NewScanner(conn)
	m.handleCommands(conn, scanner)
}

func (m *mockOpenVPNMgmt) handleCommands(conn net.Conn, scanner *bufio.Scanner) {
	for scanner.Scan() {
		cmd := strings.TrimSpace(scanner.Text())
		switch cmd {
		case "state":
			// Format: timestamp,STATE,DESC,localIP,remoteIP,port,,
			if m.state == "CONNECTED" {
				fmt.Fprintf(conn, "1234567890,%s,SUCCESS,%s,1.2.3.4,1194,,\n", m.state, m.localIP)
			} else {
				fmt.Fprintf(conn, "1234567890,%s,,,,,\n", m.state)
			}
			fmt.Fprintf(conn, "END\n")
		case "quit":
			fmt.Fprintf(conn, "BYE\n")
			return
		default:
			fmt.Fprintf(conn, "ERROR: unknown command\n")
		}
	}
}

// --- Mock mgmtDialer for unit tests that bypass TCP ---

type mockMgmtDialer struct {
	err error
	ip  net.IP
}

func (m *mockMgmtDialer) QueryLocalIP(_ context.Context) (net.IP, error) {
	return m.ip, m.err
}

// --- Unit Tests ---

func TestNewOpenVPNService(t *testing.T) {
	mgmt := newMockOpenVPNMgmt(t, "127.0.0.1", "CONNECTED")
	defer mgmt.close()

	cfg := &netapi.OpenVPNConfig{
		ManagementAddress: mgmt.addr(),
	}
	svc, err := NewOpenVPNService(cfg)
	require.NoError(t, err)
	require.NotNil(t, svc)

	// Verify it implements the Service interface
	var _ netapi.Service = svc

	// Verify the local IP was discovered
	assert.Equal(t, net.ParseIP("127.0.0.1"), svc.localIP)
}

func TestNewOpenVPNService_WithPassword(t *testing.T) {
	mgmt := newMockOpenVPNMgmt(t, "127.0.0.1", "CONNECTED", withPassword("secret123"))
	defer mgmt.close()

	cfg := &netapi.OpenVPNConfig{
		ManagementAddress:  mgmt.addr(),
		ManagementPassword: "secret123",
	}
	svc, err := NewOpenVPNService(cfg)
	require.NoError(t, err)
	require.NotNil(t, svc)
	assert.Equal(t, net.ParseIP("127.0.0.1"), svc.localIP)
}

func TestNewOpenVPNService_WrongPassword(t *testing.T) {
	mgmt := newMockOpenVPNMgmt(t, "127.0.0.1", "CONNECTED", withPassword("correct"))
	defer mgmt.close()

	cfg := &netapi.OpenVPNConfig{
		ManagementAddress:  mgmt.addr(),
		ManagementPassword: "wrong",
	}
	svc, err := NewOpenVPNService(cfg)
	assert.Nil(t, svc)
	require.Error(t, err)
}

func TestNewOpenVPNService_VPNNotConnected(t *testing.T) {
	mgmt := newMockOpenVPNMgmt(t, "", "RECONNECTING")
	defer mgmt.close()

	cfg := &netapi.OpenVPNConfig{
		ManagementAddress: mgmt.addr(),
	}
	svc, err := NewOpenVPNService(cfg)
	assert.Nil(t, svc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in CONNECTED state")
}

func TestNewOpenVPNService_MgmtUnreachable(t *testing.T) {
	cfg := &netapi.OpenVPNConfig{
		ManagementAddress: "127.0.0.1:19998",
	}
	svc, err := NewOpenVPNService(cfg)
	assert.Nil(t, svc)
	require.Error(t, err)
	assert.True(t, isConnectionError(err), "expected connection error, got: %v", err)
}

func TestNewOpenVPNService_CustomIP(t *testing.T) {
	mgmt := newMockOpenVPNMgmt(t, "10.8.0.6", "CONNECTED")
	defer mgmt.close()

	cfg := &netapi.OpenVPNConfig{
		ManagementAddress: mgmt.addr(),
	}
	svc, err := NewOpenVPNService(cfg)
	require.NoError(t, err)
	assert.Equal(t, net.ParseIP("10.8.0.6"), svc.localIP)
}

func TestOpenVPNService_DialContext(t *testing.T) {
	// Start a local server as the "destination"
	backend, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer backend.Close()
	go func() {
		for {
			c, err := backend.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("hello from vpn backend"))
			c.Close()
		}
	}()

	// Create service with mock dialer, using 127.0.0.1 as localIP
	// so the dialer can actually bind to it
	svc := newOpenVPNServiceWithMgmt(
		&mockMgmtDialer{ip: net.ParseIP("127.0.0.1")},
		net.ParseIP("127.0.0.1"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", backend.Addr().String())
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	buf := make([]byte, 100)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello from vpn backend", string(buf[:n]))
}

func TestOpenVPNService_DialContext_NilIP(t *testing.T) {
	svc := newOpenVPNServiceWithMgmt(
		&mockMgmtDialer{ip: nil},
		nil,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "example.com:80")
	assert.Nil(t, conn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no local VPN IP available")
}

func TestOpenVPNService_Listen_NotSupported(t *testing.T) {
	svc := newOpenVPNServiceWithMgmt(
		&mockMgmtDialer{ip: net.ParseIP("127.0.0.1")},
		net.ParseIP("127.0.0.1"),
	)

	ln, err := svc.Listen(context.Background(), "tcp", "0.0.0.0:8080")
	assert.Nil(t, ln)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrNotSupported)
}

func TestOpenVPNService_ListenPacket_NotSupported(t *testing.T) {
	svc := newOpenVPNServiceWithMgmt(
		&mockMgmtDialer{ip: net.ParseIP("127.0.0.1")},
		net.ParseIP("127.0.0.1"),
	)

	pc, err := svc.ListenPacket(context.Background(), "udp", "0.0.0.0:8080")
	assert.Nil(t, pc)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrNotSupported)
}

func TestOpenVPNService_LookupHost_NotSupported(t *testing.T) {
	svc := newOpenVPNServiceWithMgmt(
		&mockMgmtDialer{ip: net.ParseIP("127.0.0.1")},
		net.ParseIP("127.0.0.1"),
	)

	hosts, err := svc.LookupHost(context.Background(), "example.com")
	assert.Nil(t, hosts)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrNotSupported)
}

func TestOpenVPNService_ConcurrentDial(t *testing.T) {
	// Start a local server as the "destination"
	backend, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer backend.Close()
	go func() {
		for {
			c, err := backend.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	svc := newOpenVPNServiceWithMgmt(
		&mockMgmtDialer{ip: net.ParseIP("127.0.0.1")},
		net.ParseIP("127.0.0.1"),
	)

	const numGoroutines = 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var successCount int32
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			conn, err := svc.DialContext(ctx, "tcp", backend.Addr().String())
			if err != nil {
				return
			}
			conn.Close()
			mu.Lock()
			successCount++
			mu.Unlock()
		}()
	}

	wg.Wait()

	mu.Lock()
	count := successCount
	mu.Unlock()
	assert.Equal(t, int32(numGoroutines), count,
		"all concurrent dials should succeed when binding to 127.0.0.1")
}

func TestOpenVPNConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		wantErr string
		cfg     netapi.OpenVPNConfig
	}{
		{
			name:    "empty management address",
			cfg:     netapi.OpenVPNConfig{ManagementAddress: ""},
			wantErr: "management_address is required",
		},
		{
			name: "valid config",
			cfg:  netapi.OpenVPNConfig{ManagementAddress: "127.0.0.1:7505"},
		},
		{
			name: "valid with password",
			cfg: netapi.OpenVPNConfig{
				ManagementAddress:  "127.0.0.1:7505",
				ManagementPassword: "secret",
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

func TestOpenVPNConfig_SetMeta(t *testing.T) {
	cfg := &netapi.OpenVPNConfig{
		ManagementAddress: "127.0.0.1:7505",
	}
	meta := map[string]any{"label": "prod-vpn"}
	cfg.SetMeta(meta)
	assert.Equal(t, meta, map[string]any(cfg.Meta))
}

func TestOpenVPNService_DialContext_AddressTypes(t *testing.T) {
	// Start multiple backend servers to accept connections
	backends := make([]net.Listener, 4)
	for i := range backends {
		ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer ln.Close()
		backends[i] = ln
		go func(l net.Listener) {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}(ln)
	}

	svc := newOpenVPNServiceWithMgmt(
		&mockMgmtDialer{ip: net.ParseIP("127.0.0.1")},
		net.ParseIP("127.0.0.1"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, ln := range backends {
		conn, err := svc.DialContext(ctx, "tcp", ln.Addr().String())
		require.NoError(t, err)
		conn.Close()
	}
}

// TestRealMgmtDialer_Protocol tests the management protocol parsing
// with a real TCP mock server (full integration of realMgmtDialer).
func TestRealMgmtDialer_Protocol(t *testing.T) {
	mgmt := newMockOpenVPNMgmt(t, "10.8.0.6", "CONNECTED")
	defer mgmt.close()

	d := &realMgmtDialer{addr: mgmt.addr()}
	ip, err := d.QueryLocalIP(context.Background())
	require.NoError(t, err)
	assert.Equal(t, net.ParseIP("10.8.0.6"), ip)
}

func TestRealMgmtDialer_Protocol_WithPassword(t *testing.T) {
	mgmt := newMockOpenVPNMgmt(t, "172.16.0.1", "CONNECTED", withPassword("mgmt-pass"))
	defer mgmt.close()

	d := &realMgmtDialer{addr: mgmt.addr(), password: "mgmt-pass"}
	ip, err := d.QueryLocalIP(context.Background())
	require.NoError(t, err)
	assert.Equal(t, net.ParseIP("172.16.0.1"), ip)
}

func TestRealMgmtDialer_Protocol_NotConnected(t *testing.T) {
	mgmt := newMockOpenVPNMgmt(t, "", "RECONNECTING")
	defer mgmt.close()

	d := &realMgmtDialer{addr: mgmt.addr()}
	ip, err := d.QueryLocalIP(context.Background())
	assert.Nil(t, ip)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in CONNECTED state")
}
