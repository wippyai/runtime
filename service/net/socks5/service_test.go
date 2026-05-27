// SPDX-License-Identifier: MPL-2.0

package socks5

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	netapi "github.com/wippyai/runtime/api/net"
)

// --- Mock SOCKS5 Server ---
//
// Implements just enough of RFC 1928 + RFC 1929 (username/password auth)
// to exercise the SOCKS5 dialer. Each dial is recorded with the negotiated
// credentials so tests can assert stream-isolation behavior.

const (
	socks5Version  byte = 0x05
	socks5AuthNone byte = 0x00
	socks5AuthUP   byte = 0x02
	socks5CmdConn  byte = 0x01
	socks5ATypIPv4 byte = 0x01
	socks5ATypFQDN byte = 0x03
	socks5ATypIPv6 byte = 0x04
	socks5RepOK    byte = 0x00
	socks5RepFail  byte = 0x01
)

// socks5Record captures one accepted SOCKS5 session.
type socks5Record struct {
	Username    string
	Password    string
	DestAddress string
	DestPort    uint16
	AuthUsed    byte
}

// mockSOCKS5Server is a minimal SOCKS5 proxy for tests. After a successful
// CONNECT it bridges the client to `backend` (if set) or closes the stream.
type mockSOCKS5Server struct {
	listener   net.Listener
	backend    string
	expectUser string
	expectPass string
	records    []socks5Record
	mu         sync.Mutex
	failDial   atomic.Bool
	requireUP  bool
}

func newMockSOCKS5Server(t *testing.T) *mockSOCKS5Server {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	s := &mockSOCKS5Server{listener: ln}
	go s.serve()
	return s
}

func (s *mockSOCKS5Server) addr() string {
	return s.listener.Addr().String()
}

func (s *mockSOCKS5Server) hostPort() (string, int) {
	host, port, _ := net.SplitHostPort(s.addr())
	p, _ := strconv.Atoi(port)
	return host, p
}

func (s *mockSOCKS5Server) close() {
	s.listener.Close()
}

func (s *mockSOCKS5Server) getRecords() []socks5Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]socks5Record, len(s.records))
	copy(cp, s.records)
	return cp
}

func (s *mockSOCKS5Server) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *mockSOCKS5Server) handle(conn net.Conn) {
	defer conn.Close()

	rec := socks5Record{}

	// --- method selection ---
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return
	}
	if hdr[0] != socks5Version {
		return
	}
	nMethods := int(hdr[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}

	chosen := byte(0xFF)
	wantUP := s.requireUP
	for _, m := range methods {
		if wantUP && m == socks5AuthUP {
			chosen = socks5AuthUP
			break
		}
		if !wantUP && m == socks5AuthNone {
			chosen = socks5AuthNone
			break
		}
		if !wantUP && m == socks5AuthUP {
			chosen = socks5AuthUP
		}
	}
	if _, err := conn.Write([]byte{socks5Version, chosen}); err != nil {
		return
	}
	if chosen == 0xFF {
		return
	}
	rec.AuthUsed = chosen

	// --- optional username/password sub-negotiation ---
	if chosen == socks5AuthUP {
		uph := make([]byte, 2)
		if _, err := io.ReadFull(conn, uph); err != nil {
			return
		}
		if uph[0] != 0x01 {
			return
		}
		userLen := int(uph[1])
		userBuf := make([]byte, userLen)
		if _, err := io.ReadFull(conn, userBuf); err != nil {
			return
		}
		plBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, plBuf); err != nil {
			return
		}
		passLen := int(plBuf[0])
		passBuf := make([]byte, passLen)
		if _, err := io.ReadFull(conn, passBuf); err != nil {
			return
		}
		rec.Username = string(userBuf)
		rec.Password = string(passBuf)

		status := byte(0x00)
		if s.expectUser != "" && (rec.Username != s.expectUser || rec.Password != s.expectPass) {
			status = 0x01
		}
		if _, err := conn.Write([]byte{0x01, status}); err != nil {
			return
		}
		if status != 0x00 {
			return
		}
	}

	// --- CONNECT request ---
	reqHdr := make([]byte, 4)
	if _, err := io.ReadFull(conn, reqHdr); err != nil {
		return
	}
	if reqHdr[0] != socks5Version || reqHdr[1] != socks5CmdConn {
		s.writeReply(conn, socks5RepFail)
		return
	}

	switch reqHdr[3] {
	case socks5ATypIPv4:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return
		}
		rec.DestAddress = net.IP(addr).String()
	case socks5ATypIPv6:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return
		}
		rec.DestAddress = net.IP(addr).String()
	case socks5ATypFQDN:
		ln := make([]byte, 1)
		if _, err := io.ReadFull(conn, ln); err != nil {
			return
		}
		name := make([]byte, int(ln[0]))
		if _, err := io.ReadFull(conn, name); err != nil {
			return
		}
		rec.DestAddress = string(name)
	default:
		s.writeReply(conn, socks5RepFail)
		return
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return
	}
	rec.DestPort = binary.BigEndian.Uint16(portBuf)

	s.mu.Lock()
	s.records = append(s.records, rec)
	s.mu.Unlock()

	if s.failDial.Load() {
		s.writeReply(conn, socks5RepFail)
		return
	}

	if s.backend == "" {
		s.writeReply(conn, socks5RepOK)
		return
	}

	upstream, err := (&net.Dialer{Timeout: 3 * time.Second}).Dial("tcp", s.backend)
	if err != nil {
		s.writeReply(conn, socks5RepFail)
		return
	}
	defer upstream.Close()

	s.writeReply(conn, socks5RepOK)

	done := make(chan struct{}, 2)
	go func() { io.Copy(upstream, conn); done <- struct{}{} }()
	go func() { io.Copy(conn, upstream); done <- struct{}{} }()
	<-done
}

func (s *mockSOCKS5Server) writeReply(conn net.Conn, rep byte) {
	// BND.ADDR/PORT zero-filled.
	conn.Write([]byte{
		socks5Version, rep, 0x00, socks5ATypIPv4,
		0, 0, 0, 0,
		0, 0,
	})
}

// --- helpers ---

func newService(t *testing.T, srv *mockSOCKS5Server, cfg *netapi.SOCKS5Config) *Service {
	t.Helper()
	host, port := srv.hostPort()
	if cfg == nil {
		cfg = &netapi.SOCKS5Config{}
	}
	cfg.Host = host
	cfg.Port = port

	svc, err := NewService(cfg)
	require.NoError(t, err)
	return svc
}

// --- Service Unit Tests ---

func TestService_ImplementsInterface(t *testing.T) {
	srv := newMockSOCKS5Server(t)
	defer srv.close()
	svc := newService(t, srv, nil)
	var _ netapi.Service = svc
}

func TestNewService_Basic(t *testing.T) {
	srv := newMockSOCKS5Server(t)
	defer srv.close()

	svc := newService(t, srv, nil)
	assert.False(t, svc.isolateStreams)
	assert.Nil(t, svc.baseAuth)
}

func TestNewService_WithAuth(t *testing.T) {
	srv := newMockSOCKS5Server(t)
	defer srv.close()

	svc := newService(t, srv, &netapi.SOCKS5Config{
		Username: "user1",
		Password: "pass1",
	})
	require.NotNil(t, svc.baseAuth)
	assert.Equal(t, "user1", svc.baseAuth.User)
	assert.Equal(t, "pass1", svc.baseAuth.Password)
}

func TestNewService_WithIsolation(t *testing.T) {
	srv := newMockSOCKS5Server(t)
	defer srv.close()

	svc := newService(t, srv, &netapi.SOCKS5Config{IsolateStreams: true})
	assert.True(t, svc.isolateStreams)
}

func TestService_DialContext_HappyPath(t *testing.T) {
	// Start a backend that responds with a known payload.
	backend, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer backend.Close()
	go func() {
		for {
			c, err := backend.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("hello from backend"))
			c.Close()
		}
	}()

	srv := newMockSOCKS5Server(t)
	srv.backend = backend.Addr().String()
	defer srv.close()

	svc := newService(t, srv, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "example.com:80")
	require.NoError(t, err)
	defer conn.Close()

	buf := make([]byte, 100)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello from backend", string(buf[:n]))

	recs := srv.getRecords()
	require.Len(t, recs, 1)
	assert.Equal(t, "example.com", recs[0].DestAddress, "FQDN should be forwarded to proxy, not resolved locally")
	assert.Equal(t, uint16(80), recs[0].DestPort)
	assert.Equal(t, socks5AuthNone, recs[0].AuthUsed)
}

func TestService_DialContext_WithAuth(t *testing.T) {
	srv := newMockSOCKS5Server(t)
	srv.requireUP = true
	srv.expectUser = "alice"
	srv.expectPass = "s3cret"
	defer srv.close()

	svc := newService(t, srv, &netapi.SOCKS5Config{
		Username: "alice",
		Password: "s3cret",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "example.com:443")
	require.NoError(t, err)
	conn.Close()

	recs := srv.getRecords()
	require.Len(t, recs, 1)
	assert.Equal(t, "alice", recs[0].Username)
	assert.Equal(t, "s3cret", recs[0].Password)
}

func TestService_DialContext_StreamIsolation_UniqueCreds(t *testing.T) {
	srv := newMockSOCKS5Server(t)
	srv.requireUP = true
	defer srv.close()

	svc := newService(t, srv, &netapi.SOCKS5Config{IsolateStreams: true})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const dials = 8
	for i := 0; i < dials; i++ {
		conn, err := svc.DialContext(ctx, "tcp", fmt.Sprintf("peer-%d.example:80", i))
		require.NoError(t, err)
		conn.Close()
	}

	recs := srv.getRecords()
	require.Len(t, recs, dials)

	seen := make(map[string]struct{}, dials)
	for _, r := range recs {
		assert.NotEmpty(t, r.Username, "isolation must send random credentials")
		assert.Equal(t, r.Username, r.Password, "isolation uses same value for user and pass")
		if _, dup := seen[r.Username]; dup {
			t.Fatalf("duplicate isolation credential %q", r.Username)
		}
		seen[r.Username] = struct{}{}
	}
}

func TestService_DialContext_NoIsolation_ReusesCreds(t *testing.T) {
	srv := newMockSOCKS5Server(t)
	srv.requireUP = true
	srv.expectUser = "user"
	srv.expectPass = "pass"
	defer srv.close()

	svc := newService(t, srv, &netapi.SOCKS5Config{
		Username: "user",
		Password: "pass",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < 3; i++ {
		conn, err := svc.DialContext(ctx, "tcp", "example.com:80")
		require.NoError(t, err)
		conn.Close()
	}

	recs := srv.getRecords()
	require.Len(t, recs, 3)
	for _, r := range recs {
		assert.Equal(t, "user", r.Username)
		assert.Equal(t, "pass", r.Password)
	}
}

func TestService_DialContext_ProxyDown(t *testing.T) {
	// Bind then close to ensure the port is not accepting connections.
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	ln.Close()

	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)

	svc, err := NewService(&netapi.SOCKS5Config{Host: host, Port: p})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "example.com:80")
	assert.Nil(t, conn)
	require.Error(t, err)
}

func TestService_DialContext_ConnectRejected(t *testing.T) {
	srv := newMockSOCKS5Server(t)
	srv.failDial.Store(true)
	defer srv.close()

	svc := newService(t, srv, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "blocked.example:80")
	assert.Nil(t, conn)
	require.Error(t, err)
}

func TestService_DialContext_ContextCancelled(t *testing.T) {
	srv := newMockSOCKS5Server(t)
	defer srv.close()

	svc := newService(t, srv, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	conn, err := svc.DialContext(ctx, "tcp", "example.com:80")
	if conn != nil {
		conn.Close()
	}
	require.Error(t, err)
}

func TestService_Listen_NotSupported(t *testing.T) {
	srv := newMockSOCKS5Server(t)
	defer srv.close()

	svc := newService(t, srv, nil)

	ln, err := svc.Listen(context.Background(), "tcp", "0.0.0.0:0")
	assert.Nil(t, ln)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrNotSupported)
}

func TestService_ListenPacket_NotSupported(t *testing.T) {
	srv := newMockSOCKS5Server(t)
	defer srv.close()

	svc := newService(t, srv, nil)

	pc, err := svc.ListenPacket(context.Background(), "udp", "0.0.0.0:0")
	assert.Nil(t, pc)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrNotSupported)
}

func TestService_LookupHost_NotSupported(t *testing.T) {
	srv := newMockSOCKS5Server(t)
	defer srv.close()

	svc := newService(t, srv, nil)

	hosts, err := svc.LookupHost(context.Background(), "example.com")
	assert.Nil(t, hosts)
	require.Error(t, err)
	require.ErrorIs(t, err, netapi.ErrNotSupported)
}

func TestService_ConcurrentDial_StreamIsolation(t *testing.T) {
	srv := newMockSOCKS5Server(t)
	srv.requireUP = true
	defer srv.close()

	svc := newService(t, srv, &netapi.SOCKS5Config{IsolateStreams: true})

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			conn, err := svc.DialContext(ctx, "tcp", fmt.Sprintf("peer-%d.example:80", i))
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
		t.Errorf("dial failed: %v", err)
	}

	recs := srv.getRecords()
	require.Len(t, recs, workers)

	seen := make(map[string]int, workers)
	for _, r := range recs {
		seen[r.Username]++
	}
	assert.Equal(t, workers, len(seen), "each concurrent dial must use a unique credential")
}

func TestRandomIsolationCredential_Uniqueness(t *testing.T) {
	const n = 200
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		c, err := randomIsolationCredential()
		require.NoError(t, err)
		require.NotEmpty(t, c.User)
		assert.Equal(t, c.User, c.Password)
		if _, dup := seen[c.User]; dup {
			t.Fatalf("duplicate credential on iteration %d: %q", i, c.User)
		}
		seen[c.User] = struct{}{}
	}
}

func TestService_DialContext_IPv4Address(t *testing.T) {
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

	srv := newMockSOCKS5Server(t)
	srv.backend = backend.Addr().String()
	defer srv.close()

	svc := newService(t, srv, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "10.0.0.5:8080")
	require.NoError(t, err)
	conn.Close()

	recs := srv.getRecords()
	require.Len(t, recs, 1)
	assert.Equal(t, "10.0.0.5", recs[0].DestAddress)
	assert.Equal(t, uint16(8080), recs[0].DestPort)
}

func TestService_DialContext_AuthFailure(t *testing.T) {
	srv := newMockSOCKS5Server(t)
	srv.requireUP = true
	srv.expectUser = "alice"
	srv.expectPass = "correct"
	defer srv.close()

	svc := newService(t, srv, &netapi.SOCKS5Config{
		Username: "alice",
		Password: "wrong",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := svc.DialContext(ctx, "tcp", "example.com:80")
	assert.Nil(t, conn)
	require.Error(t, err)
}

// --- Error type + ErrNotSupported propagation ---

func TestService_NotSupportedWrapsAPISentinel(t *testing.T) {
	srv := newMockSOCKS5Server(t)
	defer srv.close()

	svc := newService(t, srv, nil)

	_, listenErr := svc.Listen(context.Background(), "tcp", "0.0.0.0:0")
	_, pktErr := svc.ListenPacket(context.Background(), "udp", "0.0.0.0:0")
	_, lookupErr := svc.LookupHost(context.Background(), "example.com")

	require.True(t, errors.Is(listenErr, netapi.ErrNotSupported))
	require.True(t, errors.Is(pktErr, netapi.ErrNotSupported))
	require.True(t, errors.Is(lookupErr, netapi.ErrNotSupported))
}
