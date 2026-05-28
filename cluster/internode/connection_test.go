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

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- MOCK CONNECTION FOR DETERMINISTIC FAILURE INJECTION ---

type mockConn struct {
	writeErr error
	reader   *io.PipeReader
	writer   *io.PipeWriter
	mu       sync.Mutex
	closed   bool
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

func (c *mockConn) Write(b []byte) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, io.ErrClosedPipe
	}
	if c.writeErr != nil {
		err := c.writeErr
		c.writeErr = nil
		return 0, err
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

func (c *mockConn) setWriteError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeErr = err
}

// --- TEST DRAIN SOURCE ---

// testDrainSource is an in-memory stand-in for the per-class outbound queues a
// NodeConnection drains in production. Tests push messages; the connection's
// writeLoop drains them via the bindDrain wiring.
type testDrainSource struct {
	notify   chan struct{}
	queue    []Outbound
	requeued []Outbound
	mu       sync.Mutex
}

func newTestDrainSource() *testDrainSource {
	return &testDrainSource{notify: make(chan struct{}, 1)}
}

func (s *testDrainSource) bind(c *NodeConnection) {
	c.bindDrain(s.notify, s.drain, s.requeue, 32)
}

func (s *testDrainSource) push(data []byte, class Class) {
	s.mu.Lock()
	s.queue = append(s.queue, Outbound{Data: data, Class: class})
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *testDrainSource) drain(n int) []Outbound {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return nil
	}
	if n > len(s.queue) {
		n = len(s.queue)
	}
	out := make([]Outbound, n)
	copy(out, s.queue[:n])
	s.queue = s.queue[n:]
	return out
}

func (s *testDrainSource) requeue(b []Outbound) {
	s.mu.Lock()
	s.requeued = append(s.requeued, b...)
	s.mu.Unlock()
}

func (s *testDrainSource) requeuedMessages() []Outbound {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Outbound(nil), s.requeued...)
}

// --- TEST HELPERS ---

// testConnPair is a handshaked NodeConnection pair, each wired to a drain source.
type testConnPair struct {
	a    *NodeConnection
	b    *NodeConnection
	srcA *testDrainSource
	srcB *testDrainSource
}

// newTestConnectionPair builds a handshaked NodeConnection pair over an
// in-memory pipe, each wired to its own testDrainSource.
func newTestConnectionPair(t *testing.T) testConnPair {
	t.Helper()
	pipeA, pipeB := net.Pipe()
	cfg := DefaultNodeConnectionConfig()
	logger := zap.NewNop()

	var connA, connB *NodeConnection
	var errA, errB error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		connA, errA = PerformClientHandshake(pipeA, cfg, logger, "node-A", "node-B")
	}()
	go func() {
		defer wg.Done()
		connB, errB = PerformServerHandshake(pipeB, cfg, logger, "node-B")
	}()

	wg.Wait()
	require.NoError(t, errA)
	require.NoError(t, errB)
	require.NotNil(t, connA)
	require.NotNil(t, connB)

	srcA := newTestDrainSource()
	srcA.bind(connA)
	srcB := newTestDrainSource()
	srcB.bind(connB)

	t.Cleanup(func() {
		// Close() is idempotent, so calling it in cleanup is safe even if the test closes it.
		connA.Close()
		connB.Close()
	})

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
	srcA.push(testMsg, ClassPGBroadcast)

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

	srcA.push([]byte{}, ClassPGBroadcast)

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
				srcA.push([]byte(fmt.Sprintf("sender-%d-msg-%d", senderID, j)), ClassPGBroadcast)
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

// TestNodeConnection_WriteFailureRequeues verifies that when a flush fails,
// the writeLoop hands the un-flushed batch back through the requeue closure so
// a subsequent connection can deliver it, and the run loop exits with
// ExitNetworkError.
func TestNodeConnection_WriteFailureRequeues(t *testing.T) {
	mockA, mockB := newMockConnPair()
	t.Cleanup(func() { _ = mockA.Close(); _ = mockB.Close() })

	nodeA := newNodeConnection(mockA, "node-B", DefaultNodeConnectionConfig(), zap.NewNop())
	t.Cleanup(nodeA.Close)

	srcA := newTestDrainSource()
	srcA.bind(nodeA)

	injectedErr := errors.New("injected physical write error")
	mockA.setWriteError(injectedErr)

	runErrA := make(chan *ConnectionError, 1)
	go func() { runErrA <- nodeA.Run(func(_ Class, _ []byte) {}) }()

	msg := []byte("unsent")
	srcA.push(msg, ClassPGBroadcast)

	select {
	case err := <-runErrA:
		require.Equal(t, ExitNetworkError, err.Reason)
		require.ErrorIs(t, err.Err, injectedErr)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for connection to fail")
	}

	requeued := srcA.requeuedMessages()
	require.Len(t, requeued, 1, "the un-flushed message must be requeued")
	require.Equal(t, msg, requeued[0].Data)
	require.Equal(t, ClassPGBroadcast, requeued[0].Class)
}
