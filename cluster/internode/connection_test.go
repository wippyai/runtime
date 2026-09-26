// SPDX-License-Identifier: MPL-2.0

package internode

import (
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
	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

// --- MOCK CONNECTION FOR DETERMINISTIC FAILURE INJECTION ---

type mockConn struct {
	reader *io.PipeReader
	writer *io.PipeWriter
	mu     sync.Mutex
	closed bool
}

func newMockConnPair() (*mockConn, *mockConn) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	conn1 := &mockConn{reader: r2, writer: w1}
	conn2 := &mockConn{reader: r1, writer: w2}
	return conn1, conn2
}

func (c *mockConn) Read(b []byte) (n int, err error) { return c.reader.Read(b) }
func (c *mockConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234}
}
func (c *mockConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5678}
}
func (c *mockConn) SetDeadline(_ time.Time) error      { return nil }
func (c *mockConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *mockConn) SetWriteDeadline(_ time.Time) error { return nil }

// Write does not hold the lock while blocked in the pipe, so Close can
// always interrupt it.
func (c *mockConn) Write(b []byte) (n int, err error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return 0, io.ErrClosedPipe
	}
	return c.writer.Write(b)
}

func (c *mockConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	// Closing a pipe can return an error if the other side is already closed,
	// which is fine in many test scenarios, so we don't assert on it here.
	_ = c.reader.Close()
	_ = c.writer.Close()
	return nil
}

// --- TEST SESSION SIDES ---

// testSessionSide is one node's session with its peer, backed by a real
// NodeStateManager. Tests queue frames; the bound connection drains them.
type testSessionSide struct {
	nsm   *NodeStateManager
	state *NodeState
	peer  cluster.NodeID
}

func newTestSessionSide(peer cluster.NodeID) *testSessionSide {
	nsm := setupStateManager()
	nsm.CreateNodeState(peer)
	return &testSessionSide{nsm: nsm, state: nsm.GetNodeState(peer), peer: peer}
}

func (s *testSessionSide) bind(c *NodeConnection) {
	c.bindSession(s.nsm, s.peer, s.state, 32)
}

func (s *testSessionSide) push(t *testing.T, data []byte, class Class) {
	t.Helper()
	require.NoError(t, s.nsm.QueueMessageClass(s.peer, data, class))
}

// pushWhenAdmitted queues a frame of a bounded class, waiting while its queue
// is full.
func (s *testSessionSide) pushWhenAdmitted(t *testing.T, data []byte, class Class) {
	t.Helper()
	for {
		err := s.nsm.QueueMessageClass(s.peer, data, class)
		if !errors.Is(err, ErrQueueFull) {
			assert.NoError(t, err)
			return
		}
		time.Sleep(100 * time.Microsecond)
	}
}

// --- TEST HELPERS ---

// testConnPair is a handshaked NodeConnection pair, each bound to its side's
// session.
type testConnPair struct {
	a    *NodeConnection
	b    *NodeConnection
	srcA *testSessionSide
	srcB *testSessionSide
}

// handshakePipe builds a handshaked NodeConnection pair over an in-memory
// pipe: a is node-A's end, b is node-B's.
func handshakePipe(t *testing.T) (a, b *NodeConnection) {
	t.Helper()
	pipeA, pipeB := net.Pipe()
	cfg := DefaultNodeConnectionConfig()
	logger := zap.NewNop()

	var errA, errB error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		a, errA = PerformClientHandshake(pipeA, cfg, logger, "node-A", testIncarnation, "node-B")
	}()
	go func() {
		defer wg.Done()
		b, errB = PerformServerHandshake(pipeB, cfg, logger, "node-B", testIncarnation+1)
	}()
	wg.Wait()
	require.NoError(t, errA)
	require.NoError(t, errB)
	t.Cleanup(func() {
		a.Close()
		b.Close()
	})
	return a, b
}

// newTestConnectionPair builds a handshaked NodeConnection pair over an
// in-memory pipe, each bound to a fresh session with the other.
func newTestConnectionPair(t *testing.T) testConnPair {
	t.Helper()
	connA, connB := handshakePipe(t)
	srcA := newTestSessionSide("node-B")
	srcA.bind(connA)
	srcB := newTestSessionSide("node-A")
	srcB.bind(connB)
	return testConnPair{a: connA, b: connB, srcA: srcA, srcB: srcB}
}

// --- TEST SUITE ---

func TestNodeConnection_SendReceive(t *testing.T) {
	p := newTestConnectionPair(t)
	nodeA, nodeB, srcA := p.a, p.b, p.srcA
	msgChan := make(chan []byte, 1)

	go func() { _ = nodeA.Run(func(_ Class, _ []byte) {}) }()
	go func() { _ = nodeB.Run(func(_ Class, msg []byte) { msgChan <- msg }) }()

	testMsg := []byte("hello, world!")
	srcA.push(t, testMsg, ClassPGBroadcast)

	select {
	case receivedMsg := <-msgChan:
		require.Equal(t, testMsg, receivedMsg)
	case <-time.After(2 * time.Second):
		t.Fatal("Test timed out waiting for message")
	}
}

func TestNodeConnection_Shutdown(t *testing.T) {
	p := newTestConnectionPair(t)
	nodeA, nodeB := p.a, p.b
	runLoopExited := make(chan *ConnectionError, 1)

	go func() { runLoopExited <- nodeB.Run(func(_ Class, _ []byte) {}) }()

	time.Sleep(50 * time.Millisecond)
	nodeA.Close()

	select {
	case err := <-runLoopExited:
		require.Error(t, err, "expected an error on peer shutdown")
		require.Equal(t, ExitPeerClosed, err.Reason, "Expected peer closed")
	case <-time.After(2 * time.Second):
		t.Fatal("Test timed out waiting for run loop to exit after peer closed.")
	}
}

func TestNodeConnection_SelfClose(t *testing.T) {
	nodeA := newTestConnectionPair(t).a
	runLoopExited := make(chan *ConnectionError, 1)

	go func() { runLoopExited <- nodeA.Run(func(_ Class, _ []byte) {}) }()

	time.Sleep(50 * time.Millisecond)
	nodeA.Close()

	select {
	case err := <-runLoopExited:
		require.Error(t, err)
		require.Equal(t, ExitCleanShutdown, err.Reason, "Expected clean shutdown")
	case <-time.After(2 * time.Second):
		t.Fatal("Test timed out waiting for run loop to exit after Close() was called.")
	}
}

func TestNodeConnection_ZeroLengthMessage(t *testing.T) {
	p := newTestConnectionPair(t)
	nodeA, nodeB, srcA := p.a, p.b, p.srcA
	msgChan := make(chan []byte, 1)

	go func() { _ = nodeA.Run(func(_ Class, _ []byte) {}) }()
	go func() { _ = nodeB.Run(func(_ Class, data []byte) { msgChan <- data }) }()

	srcA.push(t, []byte{}, ClassPGBroadcast)

	select {
	case msg := <-msgChan:
		require.NotNil(t, msg, "message should not be nil")
		require.Len(t, msg, 0, "Expected a zero-length message")
	case <-time.After(2 * time.Second):
		t.Fatal("Test timed out waiting for zero-length message")
	}
}

func TestNodeConnection_ConcurrentSend(t *testing.T) {
	p := newTestConnectionPair(t)
	nodeA, nodeB, srcA := p.a, p.b, p.srcA

	const numMessages = 5000
	const numSenders = 20
	var receivedCount int64
	doneChan := make(chan struct{})

	go func() { _ = nodeA.Run(func(_ Class, _ []byte) {}) }()
	go func() {
		_ = nodeB.Run(func(_ Class, _ []byte) {
			if atomic.AddInt64(&receivedCount, 1) == numMessages {
				close(doneChan)
			}
		})
	}()

	var sendWg sync.WaitGroup
	sendWg.Add(numSenders)
	for i := 0; i < numSenders; i++ {
		go func(senderID int) {
			defer sendWg.Done()
			for j := 0; j < numMessages/numSenders; j++ {
				assert.NoError(t, srcA.nsm.QueueMessageClass(srcA.peer, []byte(fmt.Sprintf("sender-%d-msg-%d", senderID, j)), ClassPGBroadcast))
			}
		}(i)
	}

	select {
	case <-doneChan:
	case <-time.After(10 * time.Second):
		t.Fatalf("Test timed out. Received %d of %d", atomic.LoadInt64(&receivedCount), numMessages)
	}
	sendWg.Wait()
}

func TestConnectionError_ShouldRetry(t *testing.T) {
	tests := []struct {
		name        string
		reason      ExitReason
		shouldRetry bool
	}{
		{"NetworkError should retry", ExitNetworkError, true},
		{"PeerClosed should retry", ExitPeerClosed, true},
		{"CleanShutdown should not retry", ExitCleanShutdown, false},
		{"ProtocolError should not retry", ExitProtocolError, false},
		{"Unknown should not retry", ExitUnknown, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connErr := &ConnectionError{Reason: tt.reason}
			require.Equal(t, tt.shouldRetry, connErr.ShouldRetry())
		})
	}
}

// failAfterConn fails every write once limit bytes were written.
type failAfterConn struct {
	net.Conn
	err     error
	limit   int
	written int
	mu      sync.Mutex
}

func (c *failAfterConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.written+len(b) > c.limit {
		return 0, c.err
	}
	c.written += len(b)
	return c.Conn.Write(b)
}

// A frame whose write fails stays in the session's resend ring and is
// delivered exactly once by the next connection of the same session.
func TestNodeConnection_WriteFailureKeepsFrameForNextConnection(t *testing.T) {
	sideA := newTestSessionSide("node-B")
	sideB := newTestSessionSide("node-A")
	received := make(chan []byte, 4)
	deliver := func(_ Class, msg []byte) { received <- msg }

	pipeA, pipeB := net.Pipe()
	injectedErr := errors.New("injected physical write error")
	// The RESUME frame (header plus 8-byte view) is the only write allowed.
	failing := &failAfterConn{Conn: pipeA, err: injectedErr, limit: frameHeaderSize + 8}
	connA := newNodeConnection(failing, "node-B", testIncarnation+1, DefaultNodeConnectionConfig(), zap.NewNop())
	connB := newNodeConnection(pipeB, "node-A", testIncarnation, DefaultNodeConnectionConfig(), zap.NewNop())
	sideA.bind(connA)
	sideB.bind(connB)
	runErrA := make(chan *ConnectionError, 1)
	go func() { runErrA <- connA.Run(func(Class, []byte) {}) }()
	go func() { _ = connB.Run(deliver) }()

	msg := []byte("unsent")
	sideA.push(t, msg, ClassPGBroadcast)
	select {
	case err := <-runErrA:
		require.Equal(t, ExitNetworkError, err.Reason)
		require.ErrorIs(t, err.Err, injectedErr)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for connection to fail")
	}
	connB.Close()
	sideA.state.queueMu.Lock()
	require.Equal(t, 1, sideA.state.session.ring.len(), "the unacknowledged frame stays in the ring")
	sideA.state.queueMu.Unlock()
	select {
	case got := <-received:
		t.Fatalf("frame %q delivered by the failed connection", got)
	default:
	}

	next := handshakeInto(t, sideA, sideB)
	go func() { _ = next.a.Run(func(Class, []byte) {}) }()
	go func() { _ = next.b.Run(deliver) }()
	select {
	case got := <-received:
		require.Equal(t, msg, got)
	case <-time.After(2 * time.Second):
		t.Fatal("frame was not replayed on the next connection")
	}
	select {
	case got := <-received:
		t.Fatalf("frame %q delivered twice", got)
	case <-time.After(100 * time.Millisecond):
	}
}

// handshakeInto builds a new pipe connection pair bound to existing sessions.
func handshakeInto(t *testing.T, sideA, sideB *testSessionSide) testConnPair {
	t.Helper()
	connA, connB := handshakePipe(t)
	sideA.bind(connA)
	sideB.bind(connB)
	return testConnPair{a: connA, b: connB, srcA: sideA, srcB: sideB}
}
