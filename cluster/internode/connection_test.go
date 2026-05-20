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
	"github.com/wippyai/runtime/api/cluster"
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

// --- TEST HELPERS ---

func newTestConnectionPair(t *testing.T, _, _ cluster.NodeID) (Connection, Connection) {
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

	t.Cleanup(func() {
		// Close() is idempotent, so calling it in cleanup is safe even if the test closes it.
		connA.Close()
		connB.Close()
	})

	return connA, connB
}

// --- TEST SUITE ---

func TestNodeConnection_SendReceive(t *testing.T) {
	nodeA, nodeB := newTestConnectionPair(t, "node-A", "node-B")
	msgChan := make(chan []byte, 1)

	go func() { _ = nodeA.Run(func(_ Class, _ []byte) {}) }()
	go func() { _ = nodeB.Run(func(_ Class, msg []byte) { msgChan <- msg }) }()

	testMsg := []byte("hello, world!")
	require.NoError(t, nodeA.Send(testMsg, ClassPGBroadcast))

	select {
	case receivedMsg := <-msgChan:
		require.Equal(t, testMsg, receivedMsg)
	case <-time.After(2 * time.Second):
		t.Fatal("Test timed out waiting for message")
	}
}

func TestNodeConnection_Shutdown(t *testing.T) {
	nodeA, nodeB := newTestConnectionPair(t, "node-A", "node-B")
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
	nodeA, _ := newTestConnectionPair(t, "node-A", "node-B")
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
	nodeA, nodeB := newTestConnectionPair(t, "node-A", "node-B")
	msgChan := make(chan []byte, 1)

	go func() { _ = nodeA.Run(func(_ Class, _ []byte) {}) }()
	go func() { _ = nodeB.Run(func(_ Class, data []byte) { msgChan <- data }) }()

	require.NoError(t, nodeA.Send([]byte{}, ClassPGBroadcast))

	select {
	case msg := <-msgChan:
		require.NotNil(t, msg, "message should not be nil")
		require.Len(t, msg, 0, "Expected a zero-length message")
	case <-time.After(2 * time.Second):
		t.Fatal("Test timed out waiting for zero-length message")
	}
}

func TestNodeConnection_ConcurrentSend(t *testing.T) {
	nodeA, nodeB := newTestConnectionPair(t, "node-A", "node-B")

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
				msg := []byte(fmt.Sprintf("sender-%d-msg-%d", senderID, j))
				if err := nodeA.Send(msg, ClassPGBroadcast); errors.Is(err, ErrConnectionClosed) {
					return
				}
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

func TestNodeConnection_ErrorOnSendToClosed(t *testing.T) {
	nodeA, _ := newTestConnectionPair(t, "node-A", "node-B")
	nodeA.Close()
	time.Sleep(10 * time.Millisecond)

	err := nodeA.Send([]byte("this should fail"), ClassPGBroadcast)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrConnectionClosed)
}

func TestNodeConnection_SendQueueCap(t *testing.T) {
	// Verify the activeQueue cap added in P2.6 actually rejects sends past
	// the configured MaxQueueSize. Regression test for the unbounded
	// container/list growth that drove the chaos-time OOMKills.
	cfg := NodeConnectionConfig{
		HandshakeTimeout: time.Second,
		MaxMessageSize:   1024,
		MaxQueueSize:     8,
	}
	conn, _ := net.Pipe()
	defer func() { _ = conn.Close() }()
	nc := newNodeConnection(conn, "peer", cfg, zap.NewNop())

	// Don't call Run() — without a writeLoop draining, every successful
	// Send leaves the message in activeQueue and fills it.
	for i := 0; i < cfg.MaxQueueSize; i++ {
		require.NoError(t, nc.Send([]byte("x"), ClassPGBroadcast), "send #%d", i)
	}
	err := nc.Send([]byte("overflow"), ClassPGBroadcast)
	require.ErrorIs(t, err, ErrQueueFull)
}

func TestNodeConnection_SendQueueCapZeroMeansUnbounded(t *testing.T) {
	// MaxQueueSize=0 keeps the legacy unbounded behavior for tests that
	// don't care about backpressure. Without this carve-out the change
	// from P2.6 would silently break unrelated tests.
	cfg := NodeConnectionConfig{
		HandshakeTimeout: time.Second,
		MaxMessageSize:   1024,
		MaxQueueSize:     0,
	}
	conn, _ := net.Pipe()
	defer func() { _ = conn.Close() }()
	nc := newNodeConnection(conn, "peer", cfg, zap.NewNop())

	for i := 0; i < 100; i++ {
		require.NoError(t, nc.Send([]byte("x"), ClassPGBroadcast))
	}
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

func TestNodeConnection_FailureAndMessageExtraction(t *testing.T) {
	mockA, mockB := newMockConnPair()
	cfg := DefaultNodeConnectionConfig()
	logger := zap.NewNop()

	nodeA := newNodeConnection(mockA, "node-B", cfg, logger)
	nodeB := newNodeConnection(mockB, "node-A", cfg, logger)
	t.Cleanup(func() { nodeA.Close(); nodeB.Close() })

	runErrA := make(chan *ConnectionError, 1)

	go func() { _ = nodeB.Run(func(_ Class, _ []byte) {}) }()
	go func() { runErrA <- nodeA.Run(func(_ Class, _ []byte) {}) }()

	msg1 := []byte("unsent-1")
	require.NoError(t, nodeA.Send(msg1, ClassPGBroadcast))

	time.Sleep(50 * time.Millisecond)
	injectedErr := errors.New("injected physical write error")
	mockA.setWriteError(injectedErr)

	msg2 := []byte("unsent-2")
	require.NoError(t, nodeA.Send(msg2, ClassPGBroadcast))

	select {
	case err := <-runErrA:
		require.Equal(t, ExitNetworkError, err.Reason)
		require.ErrorIs(t, err.Err, injectedErr)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for connection to fail")
	}

	pending := nodeA.ExtractPendingMessages()
	require.GreaterOrEqual(t, len(pending), 1, "at least one message should be recovered")
	if len(pending) == 2 {
		require.Equal(t, msg1, pending[0].Data)
		require.Equal(t, ClassPGBroadcast, pending[0].Class)
		require.Equal(t, msg2, pending[1].Data)
		require.Equal(t, ClassPGBroadcast, pending[1].Class)
	} else {
		require.Equal(t, msg2, pending[0].Data, "if only one message is pending, it must be the one sent after the write error")
		require.Equal(t, ClassPGBroadcast, pending[0].Class)
	}
}
