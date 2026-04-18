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

// --- Listener-aware mock SAM bridge ---------------------------------------

// listenerSAM simulates a SAM v3 bridge with enough features to exercise
// samListener: SESSION CREATE, NAMING LOOKUP NAME=ME, STREAM ACCEPT, and
// STREAM CONNECT that brokers a connect to an outstanding ACCEPT.
type listenerSAM struct {
	listener     net.Listener
	onAcceptHold chan struct{}
	ourDest      string
	accepts      []chan net.Conn
	mu           sync.Mutex
}

func newListenerSAM(t *testing.T) *listenerSAM {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := &listenerSAM{listener: ln, ourDest: "listener-destination-key"}
	go s.serve(t)
	return s
}

func (s *listenerSAM) addr() string { return s.listener.Addr().String() }

func (s *listenerSAM) close() { s.listener.Close() }

func (s *listenerSAM) serve(t *testing.T) {
	t.Helper()
	for {
		c, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(t, c)
	}
}

func (s *listenerSAM) handle(t *testing.T, c net.Conn) {
	t.Helper()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	reader := bufio.NewReader(c)

	// HELLO VERSION
	line, err := reader.ReadString('\n')
	if err != nil {
		c.Close()
		return
	}
	if !strings.HasPrefix(line, "HELLO VERSION") {
		c.Close()
		return
	}
	fmt.Fprintf(c, "HELLO REPLY RESULT=OK VERSION=3.3\n")

	line, err = reader.ReadString('\n')
	if err != nil {
		c.Close()
		return
	}
	line = strings.TrimRight(line, "\r\n")

	switch {
	case strings.HasPrefix(line, "SESSION CREATE"):
		s.handleControl(c, reader)
	case strings.HasPrefix(line, "STREAM ACCEPT"):
		s.handleAccept(c, reader, line)
	case strings.HasPrefix(line, "STREAM CONNECT"):
		s.handleConnect(c, line)
	default:
		c.Close()
	}
}

func (s *listenerSAM) handleControl(c net.Conn, reader *bufio.Reader) {
	fmt.Fprintf(c, "SESSION STATUS RESULT=OK DESTINATION=%s\n", s.ourDest)
	c.SetDeadline(time.Time{})
	// Reply to NAMING LOOKUP NAME=ME if asked; otherwise keep the control
	// connection open until client closes it.
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			c.Close()
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, "NAMING LOOKUP") {
			fmt.Fprintf(c, "NAMING REPLY RESULT=OK NAME=ME VALUE=%s\n", s.ourDest)
		}
	}
}

func (s *listenerSAM) handleAccept(c net.Conn, reader *bufio.Reader, _ string) {
	// Optional hook for Close-while-accepting.
	if s.onAcceptHold != nil {
		<-s.onAcceptHold
	}

	fmt.Fprintf(c, "STREAM STATUS RESULT=OK\n")
	ch := make(chan net.Conn, 1)
	s.mu.Lock()
	s.accepts = append(s.accepts, ch)
	s.mu.Unlock()

	// Watch for the client closing the accept socket before a peer arrives
	// (listener.Close path). A probe Read on c would steal application
	// bytes once a peer starts writing, so we only enable it until a peer
	// connects, then disable it by setting a past deadline so the probe
	// goroutine exits promptly.
	closedCh := make(chan struct{})
	go func() {
		buf := make([]byte, 1)
		if _, err := c.Read(buf); err != nil {
			select {
			case ch <- nil:
			default:
			}
		}
		close(closedCh)
	}()

	peer := <-ch
	// Stop the probe goroutine before doing anything further on c.
	c.SetReadDeadline(time.Unix(1, 0))
	<-closedCh
	c.SetReadDeadline(time.Time{})
	// Refresh the bufio reader so stale buffered bytes from the probe don't
	// interfere with the bridge. In practice the probe reads 0 bytes on
	// close/deadline-hit so this is a safety net.
	reader = bufio.NewReader(c)

	if peer == nil {
		c.Close()
		return
	}
	// Emit peer destination header line as SAM would.
	fmt.Fprintf(c, "peer-destination-key FROM_PORT=0 TO_PORT=0\n")
	// Bridge bytes until either side closes.
	bridge := make(chan struct{}, 2)
	go func() { io.Copy(c, peer); bridge <- struct{}{} }()
	go func() { io.Copy(peer, reader); bridge <- struct{}{} }()
	<-bridge
	peer.Close()
	c.Close()
}

func (s *listenerSAM) handleConnect(c net.Conn, _ string) {
	fmt.Fprintf(c, "STREAM STATUS RESULT=OK\n")
	// Hand the socket to the first pending ACCEPT.
	s.mu.Lock()
	var ch chan net.Conn
	if len(s.accepts) > 0 {
		ch = s.accepts[0]
		s.accepts = s.accepts[1:]
	}
	s.mu.Unlock()
	if ch == nil {
		c.Close()
		return
	}
	ch <- c
}

// clientDial writes bytes to the SAM bridge and simulates an inbound I2P peer
// initiating a STREAM CONNECT. Returns the client side of the bridged TCP
// connection so the test can write/read application bytes.
func (s *listenerSAM) clientDial(t *testing.T) net.Conn {
	t.Helper()
	c, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", s.addr())
	require.NoError(t, err)
	fmt.Fprintf(c, "HELLO VERSION MIN=3.0 MAX=3.3\n")
	r := bufio.NewReader(c)
	line, err := r.ReadString('\n')
	require.NoError(t, err)
	require.Contains(t, line, "RESULT=OK")
	fmt.Fprintf(c, "STREAM CONNECT ID=ignored DESTINATION=peer SILENT=false\n")
	line, err = r.ReadString('\n')
	require.NoError(t, err)
	require.Contains(t, line, "RESULT=OK")
	return &bufferedConn{Conn: c, reader: r}
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

// --- Tests -------------------------------------------------------------

func TestI2PService_Listen_SessionCreated(t *testing.T) {
	sam := newListenerSAM(t)
	defer sam.close()

	host, portStr, err := net.SplitHostPort(sam.addr())
	require.NoError(t, err)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
		SessionName:   "listen-test",
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	ln, err := svc.Listen(context.Background(), "tcp", "0.0.0.0:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	// Addr reflects our destination key from NAMING LOOKUP NAME=ME.
	addr := ln.Addr()
	assert.Equal(t, "i2p", addr.Network())
	assert.Equal(t, sam.ourDest, addr.String())
}

func TestI2PService_Accept_DeliversPeer(t *testing.T) {
	sam := newListenerSAM(t)
	defer sam.close()

	host, portStr, err := net.SplitHostPort(sam.addr())
	require.NoError(t, err)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
		SessionName:   "accept-test",
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	ln, err := svc.Listen(context.Background(), "tcp", "0.0.0.0:0")
	require.NoError(t, err)
	defer ln.Close()

	// Server goroutine: accept and echo.
	acceptErr := make(chan error, 1)
	acceptedMsg := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		defer conn.Close()
		// Peer addr comes from the destination header.
		remote := conn.RemoteAddr()
		assert.Equal(t, "i2p", remote.Network())
		buf := make([]byte, 64)
		n, err := conn.Read(buf)
		if err != nil {
			acceptErr <- err
			return
		}
		acceptedMsg <- string(buf[:n])
		_, _ = conn.Write([]byte("server-reply"))
	}()

	// Give the ACCEPT a moment to register.
	time.Sleep(50 * time.Millisecond)

	client := sam.clientDial(t)
	defer client.Close()
	_, err = client.Write([]byte("hello-from-peer"))
	require.NoError(t, err)

	select {
	case msg := <-acceptedMsg:
		assert.Equal(t, "hello-from-peer", msg)
	case err := <-acceptErr:
		t.Fatalf("Accept failed: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("Accept did not deliver peer bytes in time")
	}

	buf := make([]byte, 64)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := client.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "server-reply", string(buf[:n]))
}

func TestI2PService_Close_AbortsPendingAccept(t *testing.T) {
	sam := newListenerSAM(t)
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

	ln, err := svc.Listen(context.Background(), "tcp", "0.0.0.0:0")
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		_, err := ln.Accept()
		errCh <- err
	}()

	time.Sleep(100 * time.Millisecond)
	require.NoError(t, ln.Close())

	select {
	case err := <-errCh:
		require.Error(t, err)
		assert.ErrorIs(t, err, net.ErrClosed)
	case <-time.After(2 * time.Second):
		t.Fatal("Accept did not return after Close")
	}
}

func TestI2PService_Accept_AfterClose(t *testing.T) {
	sam := newListenerSAM(t)
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

	ln, err := svc.Listen(context.Background(), "tcp", "0.0.0.0:0")
	require.NoError(t, err)
	require.NoError(t, ln.Close())

	_, err = ln.Accept()
	require.Error(t, err)
	assert.ErrorIs(t, err, net.ErrClosed)
}

func TestI2PService_Listen_Concurrent(t *testing.T) {
	// Two concurrent listeners on the same SAM bridge must get distinct
	// session IDs (listenerCounter increments).
	sam := newListenerSAM(t)
	defer sam.close()

	host, portStr, err := net.SplitHostPort(sam.addr())
	require.NoError(t, err)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := &netapi.I2PConfig{
		NetworkConfig: netapi.NetworkConfig{Host: host, Port: port},
		SessionName:   "multi-listen",
	}
	svc, err := NewService(cfg)
	require.NoError(t, err)

	ln1, err := svc.Listen(context.Background(), "tcp", "0.0.0.0:0")
	require.NoError(t, err)
	defer ln1.Close()

	ln2, err := svc.Listen(context.Background(), "tcp", "0.0.0.0:0")
	require.NoError(t, err)
	defer ln2.Close()

	s1 := ln1.(*samListener).sessionID
	s2 := ln2.(*samListener).sessionID
	assert.NotEqual(t, s1, s2, "concurrent listeners must have unique session IDs")
	assert.True(t, strings.HasPrefix(s1, "multi-listen-listen-"))
	assert.True(t, strings.HasPrefix(s2, "multi-listen-listen-"))
}
