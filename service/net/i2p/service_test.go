// SPDX-License-Identifier: MPL-2.0

package i2p

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	netapi "github.com/wippyai/runtime/api/net"
)

// --- Mock SAM v3 Server ---

// samConnRecord records a single SAM session + stream connect attempt.
type samConnRecord struct {
	SessionID       string
	SessionStyle    string
	StreamDest      string
	StreamSessionID string
	HelloReceived   bool
}

// mockSAMServer is a minimal SAM v3 bridge for testing.
// It records all handshake attempts and optionally forwards to a backend.
type mockSAMServer struct {
	listener net.Listener

	// backend address to forward to after successful STREAM CONNECT.
	// If empty, returns OK but closes immediately.
	backend string

	// Custom error result for the failing step
	failResult string

	records []samConnRecord

	// If set, writes STREAM STATUS reply and this payload in a single Write
	// so the client's bufio.Reader buffers both together. Exercises the
	// CI-flaky path where TCP coalesces the handshake reply with the first
	// application bytes.
	streamCoalescedPayload []byte

	mu sync.Mutex

	// Error injection: which step to fail at (0=none, 1=hello, 2=session, 3=stream)
	failAtStep int
}

func newMockSAMServer(t *testing.T) *mockSAMServer {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	s := &mockSAMServer{listener: ln, failResult: "I2P_ERROR"}
	go s.serve(t)
	return s
}

func (s *mockSAMServer) addr() string {
	return s.listener.Addr().String()
}

func (s *mockSAMServer) close() {
	s.listener.Close()
}

func (s *mockSAMServer) getRecords() []samConnRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]samConnRecord, len(s.records))
	copy(cp, s.records)
	return cp
}

func (s *mockSAMServer) serve(t *testing.T) {
	t.Helper()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return // listener closed
		}
		go s.handleConn(t, conn)
	}
}

func (s *mockSAMServer) handleConn(t *testing.T, conn net.Conn) {
	t.Helper()
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	reader := bufio.NewReader(conn)

	// --- Step 1: HELLO VERSION (common to all connections) ---
	line, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "HELLO VERSION") {
		return
	}

	if s.failAtStep == 1 {
		// Record before writing the response. The client can observe the
		// rejection and return before this goroutine continues on a fast CI
		// runner, so recording after the write makes the assertion flaky.
		s.recordConn(samConnRecord{HelloReceived: true})
		fmt.Fprintf(conn, "HELLO REPLY RESULT=%s MESSAGE=\"test failure\"\n", s.failResult)
		return
	}
	fmt.Fprintf(conn, "HELLO REPLY RESULT=OK VERSION=3.3\n")

	// --- Step 2: Read next command (SESSION CREATE or STREAM CONNECT) ---
	line, err = reader.ReadString('\n')
	if err != nil {
		s.recordConn(samConnRecord{HelloReceived: true})
		return
	}
	line = strings.TrimRight(line, "\r\n")

	if strings.HasPrefix(line, "SESSION CREATE") {
		// --- Control connection: handle SESSION CREATE ---
		rec := samConnRecord{HelloReceived: true}
		rec.SessionID = samExtractField(line, "ID=")
		rec.SessionStyle = samExtractField(line, "STYLE=")

		// Record BEFORE writing response to avoid race with test assertions.
		s.recordConn(rec)

		if s.failAtStep == 2 {
			fmt.Fprintf(conn, "SESSION STATUS RESULT=%s MESSAGE=\"session rejected\"\n", s.failResult)
			return
		}
		fmt.Fprintf(conn, "SESSION STATUS RESULT=OK DESTINATION=mock-destination-key\n")

		// Keep the control connection alive until the client closes it.
		// The SAM session is tied to this connection's lifetime.
		conn.SetDeadline(time.Time{})
		buf := make([]byte, 1)
		for {
			if _, err := conn.Read(buf); err != nil {
				break
			}
		}
	} else if strings.HasPrefix(line, "STREAM CONNECT") {
		// --- Stream connection: handle STREAM CONNECT ---
		rec := samConnRecord{HelloReceived: true}
		rec.StreamSessionID = samExtractField(line, "ID=")
		rec.StreamDest = samExtractField(line, "DESTINATION=")

		// Record BEFORE writing response to avoid race with test assertions.
		s.recordConn(rec)

		if s.failAtStep == 3 {
			fmt.Fprintf(conn, "STREAM STATUS RESULT=%s MESSAGE=\"connect rejected\"\n", s.failResult)
			return
		}
		if len(s.streamCoalescedPayload) > 0 {
			buf := append([]byte("STREAM STATUS RESULT=OK\n"), s.streamCoalescedPayload...)
			conn.Write(buf)
			return
		}
		fmt.Fprintf(conn, "STREAM STATUS RESULT=OK\n")

		// If backend is configured, bridge to it
		if s.backend != "" {
			upstream, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(context.Background(), "tcp", s.backend)
			if err != nil {
				return
			}
			defer upstream.Close()

			conn.SetDeadline(time.Time{})
			done := make(chan struct{})
			go func() {
				io.Copy(upstream, reader)
				close(done)
			}()
			io.Copy(conn, upstream)
			<-done
		}
	}
}

func (s *mockSAMServer) recordConn(rec samConnRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, rec)
}

// samExtractField extracts a value like "ID=foo" from a SAM protocol line.
func samExtractField(line, key string) string {
	idx := strings.Index(line, key)
	if idx < 0 {
		return ""
	}
	rest := line[idx+len(key):]
	if sp := strings.IndexByte(rest, ' '); sp >= 0 {
		return rest[:sp]
	}
	return rest
}

// --- I2P Service Tests ---

func TestNewService(t *testing.T) {
	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 7656},
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)
	require.NotNil(t, svc)

	// Verify interface
	var _ netapi.Service = svc

	// Default session name
	assert.Equal(t, "wippy", svc.sessionName)
}

func TestNewService_CustomSessionName(t *testing.T) {
	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 7656},
		SessionName:   "my-session",
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)
	assert.Equal(t, "my-session", svc.sessionName)
}

func TestI2PService_DialContext(t *testing.T) {
	// Start a backend to forward data through the mock SAM bridge
	backend, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer backend.Close()
	go func() {
		for {
			c, err := backend.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("i2p-backend-response"))
			c.Close()
		}
	}()

	sam := newMockSAMServer(t)
	defer sam.close()
	sam.backend = backend.Addr().String()

	host, portStr, err := net.SplitHostPort(sam.addr())
	require.NoError(t, err)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
		SessionName:   "test-dial",
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "target.i2p:80")
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	// Read the backend response through the SAM bridge
	buf := make([]byte, 100)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "i2p-backend-response", string(buf[:n]))

	// Verify SAM protocol was followed correctly (two-connection protocol)
	recs := sam.getRecords()
	require.Len(t, recs, 2, "should have control + stream records")

	// Find control and stream records
	var ctrl, stream *samConnRecord
	for i := range recs {
		if recs[i].SessionID != "" {
			ctrl = &recs[i]
		}
		if recs[i].StreamDest != "" {
			stream = &recs[i]
		}
	}
	require.NotNil(t, ctrl, "should have a control record")
	require.NotNil(t, stream, "should have a stream record")

	assert.True(t, ctrl.HelloReceived, "control HELLO should be sent")
	assert.True(t, strings.HasPrefix(ctrl.SessionID, "test-dial-"),
		"session ID should start with base name, got: %s", ctrl.SessionID)
	assert.Equal(t, "STREAM", ctrl.SessionStyle)

	assert.True(t, stream.HelloReceived, "stream HELLO should be sent")
	assert.Equal(t, "target.i2p", stream.StreamDest, "port should be stripped from DESTINATION")
	assert.Equal(t, ctrl.SessionID, stream.StreamSessionID,
		"stream should reference the control session")
}

// TestI2PService_DialContext_PayloadCoalescedWithStatus verifies that payload
// bytes buffered by the handshake bufio.Reader (when TCP coalesces STREAM
// STATUS with the first application segment) are still delivered to the
// caller via the returned net.Conn. Without buffered-reader forwarding the
// client's Read hangs forever — the exact failure seen on CI ubuntu-latest.
func TestI2PService_DialContext_PayloadCoalescedWithStatus(t *testing.T) {
	payload := []byte("post-handshake-bytes")

	sam := newMockSAMServer(t)
	sam.streamCoalescedPayload = payload
	defer sam.close()

	host, portStr, err := net.SplitHostPort(sam.addr())
	require.NoError(t, err)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
		SessionName:   "test-coalesced",
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "target.i2p:80")
	require.NoError(t, err)
	defer conn.Close()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	buf := make([]byte, len(payload))
	n, err := io.ReadFull(conn, buf)
	require.NoError(t, err)
	assert.Equal(t, payload, buf[:n])
}

func TestI2PService_DialContext_B32Address(t *testing.T) {
	sam := newMockSAMServer(t)
	defer sam.close()

	host, portStr, err := net.SplitHostPort(sam.addr())
	require.NoError(t, err)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test with a .b32.i2p address (base32 encoded destination)
	b32Addr := "ukeu3k5oycga3uneqgtnvselmt4yemvoilkln7jpvamvfx7dnkdq.b32.i2p:443"
	b32Host := "ukeu3k5oycga3uneqgtnvselmt4yemvoilkln7jpvamvfx7dnkdq.b32.i2p"
	conn, err := svc.DialContext(ctx, "tcp", b32Addr)
	if conn != nil {
		conn.Close()
	}
	// Connection completes at SAM level (mock returns OK)
	_ = err

	recs := sam.getRecords()
	require.Len(t, recs, 2, "should have control + stream records")

	// Find stream record
	var stream *samConnRecord
	for i := range recs {
		if recs[i].StreamDest != "" {
			stream = &recs[i]
		}
	}
	require.NotNil(t, stream, "should have a stream record")
	assert.Equal(t, b32Host, stream.StreamDest,
		"b32.i2p address should be passed without port to SAM DESTINATION")
	_ = b32Addr
}

func TestI2PService_DialContext_SAMDown(t *testing.T) {
	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 19999},
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "target.i2p:80")
	assert.Nil(t, conn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SAM bridge")
}

func TestI2PService_DialContext_HelloFailed(t *testing.T) {
	sam := newMockSAMServer(t)
	sam.failAtStep = 1
	sam.failResult = "NOVERSION"
	defer sam.close()

	host, portStr, err := net.SplitHostPort(sam.addr())
	require.NoError(t, err)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "target.i2p:80")
	assert.Nil(t, conn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SAM handshake")

	recs := sam.getRecords()
	require.Len(t, recs, 1)
	assert.True(t, recs[0].HelloReceived)
}

func TestI2PService_DialContext_SessionRejected(t *testing.T) {
	sam := newMockSAMServer(t)
	sam.failAtStep = 2
	sam.failResult = "DUPLICATED_ID"
	defer sam.close()

	host, portStr, err := net.SplitHostPort(sam.addr())
	require.NoError(t, err)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "target.i2p:80")
	assert.Nil(t, conn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SESSION CREATE rejected")
}

func TestI2PService_DialContext_StreamConnectRejected(t *testing.T) {
	sam := newMockSAMServer(t)
	sam.failAtStep = 3
	sam.failResult = "CANT_REACH_PEER"
	defer sam.close()

	host, portStr, err := net.SplitHostPort(sam.addr())
	require.NoError(t, err)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "unreachable.i2p:80")
	assert.Nil(t, conn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "STREAM CONNECT rejected")
}

func TestI2PService_Listen_SAMDown(t *testing.T) {
	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 19999},
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	ln, err := svc.Listen(ctx, "tcp", "0.0.0.0:0")
	assert.Nil(t, ln)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SAM bridge")
}

func TestI2PService_ListenPacket_NotSupported(t *testing.T) {
	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 7656},
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	pc, err := svc.ListenPacket(context.Background(), "udp", "0.0.0.0:8080")
	assert.Nil(t, pc)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrNotSupported)
}

func TestI2PService_LookupHost_NotSupported(t *testing.T) {
	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 7656},
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	hosts, err := svc.LookupHost(context.Background(), "example.i2p")
	assert.Nil(t, hosts)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrNotSupported)
}

func TestI2PService_DialContext_MultipleConnections(t *testing.T) {
	// Each DialContext creates a fresh SAM session (TRANSIENT destination)
	sam := newMockSAMServer(t)
	defer sam.close()

	host, portStr, err := net.SplitHostPort(sam.addr())
	require.NoError(t, err)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
		SessionName:   "multi-conn",
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	destinations := []string{
		"site1.i2p:80",
		"site2.i2p:443",
		"site3.i2p:8080",
	}

	for _, dest := range destinations {
		conn, err := svc.DialContext(ctx, "tcp", dest)
		if conn != nil {
			conn.Close()
		}
		_ = err // some may fail (no backend), that's OK
	}

	recs := sam.getRecords()
	require.Len(t, recs, len(destinations)*2,
		"each dial creates 2 connections (control + stream)")

	// Collect stream records
	var streamRecs []samConnRecord
	for _, rec := range recs {
		if rec.StreamDest != "" {
			streamRecs = append(streamRecs, rec)
		}
	}
	require.Len(t, streamRecs, len(destinations))

	// Verify all destinations were reached (order may vary).
	// Port is stripped from DESTINATION in SAM STREAM CONNECT.
	destSeen := make(map[string]bool)
	for _, rec := range streamRecs {
		destSeen[rec.StreamDest] = true
	}
	expectedHosts := []string{"site1.i2p", "site2.i2p", "site3.i2p"}
	for _, host := range expectedHosts {
		assert.True(t, destSeen[host], "destination %s should be reached", host)
	}

	// Verify all control records use the base session name prefix
	for _, rec := range recs {
		if rec.SessionID != "" {
			assert.True(t, strings.HasPrefix(rec.SessionID, "multi-conn-"),
				"session ID should start with base name, got: %s", rec.SessionID)
		}
	}
}

// --- SAM protocol helper tests ---

func TestSamReadLine(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "HELLO REPLY RESULT=OK\n", "HELLO REPLY RESULT=OK"},
		{"with CR", "HELLO REPLY RESULT=OK\r\n", "HELLO REPLY RESULT=OK"},
		{"with data after", "LINE1\nLINE2\n", "LINE1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := bufio.NewReader(strings.NewReader(tc.input))
			got, err := samReadLine(r)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSamParseResult(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantVal string
		wantOK  bool
	}{
		{"ok", "HELLO REPLY RESULT=OK VERSION=3.1", "OK", true},
		{"error", "SESSION STATUS RESULT=I2P_ERROR MESSAGE=fail", "I2P_ERROR", true},
		{"noversion", "HELLO REPLY RESULT=NOVERSION", "NOVERSION", true},
		{"no result", "HELLO REPLY VERSION=3.1", "", false},
		{"empty line", "", "", false},
		{"result at end", "HELLO REPLY RESULT=OK", "OK", true},
		{"duplicated id", "SESSION STATUS RESULT=DUPLICATED_ID", "DUPLICATED_ID", true},
		{"cant reach peer", "STREAM STATUS RESULT=CANT_REACH_PEER", "CANT_REACH_PEER", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			val, ok := samParseResult(tc.line)
			assert.Equal(t, tc.wantOK, ok)
			if ok {
				assert.Equal(t, tc.wantVal, val)
			}
		})
	}
}

func TestI2PService_DialContext_ContextCancelled(t *testing.T) {
	// Create a SAM server that is slow to respond
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Don't respond — simulate a slow/unresponsive SAM bridge
			time.Sleep(10 * time.Second)
			conn.Close()
		}
	}()

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "slow-target.i2p:80")
	assert.Nil(t, conn)
	require.Error(t, err)
	// Should fail with context deadline exceeded or a read timeout
}

func TestI2PService_ConcurrentDial(t *testing.T) {
	sam := newMockSAMServer(t)
	defer sam.close()

	host, portStr, err := net.SplitHostPort(sam.addr())
	require.NoError(t, err)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
		SessionName:   "concurrent",
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	const numGoroutines = 15
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			conn, err := svc.DialContext(ctx, "tcp", fmt.Sprintf("site%d.i2p:80", idx))
			if conn != nil {
				conn.Close()
			}
			_ = err
		}(i)
	}

	wg.Wait()

	recs := sam.getRecords()
	assert.Len(t, recs, numGoroutines*2,
		"each concurrent dial creates 2 connections (control + stream)")
}

func TestI2PConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		wantErr string
		cfg     netapi.I2PConfig
	}{
		{
			name:    "empty host",
			cfg:     netapi.I2PConfig{NetworkConfig: netapi.NetworkConfig{Host: "", Port: 7656}},
			wantErr: "host is required",
		},
		{
			name:    "zero port",
			cfg:     netapi.I2PConfig{NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 0}},
			wantErr: "invalid port",
		},
		{
			name:    "negative port",
			cfg:     netapi.I2PConfig{NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: -1}},
			wantErr: "invalid port",
		},
		{
			name:    "port too large",
			cfg:     netapi.I2PConfig{NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 70000}},
			wantErr: "invalid port",
		},
		{
			name: "valid config",
			cfg:  netapi.I2PConfig{NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 7656}},
		},
		{
			name: "valid config with session",
			cfg:  netapi.I2PConfig{NetworkConfig: netapi.NetworkConfig{Host: "127.0.0.1", Port: 7656}, SessionName: "custom"},
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
