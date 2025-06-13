// file: connection_test.go
package internode

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/cluster"
	"go.uber.org/zap"
)

// setupTestConnections creates a pair of connected NodeConnection instances using net.Pipe.
func setupTestConnections(t *testing.T, nodeA_ID, nodeB_ID cluster.NodeID, cfg NodeConnectionConfig) (*NodeConnection, *NodeConnection) {
	t.Helper()
	connA, connB := net.Pipe()
	logger := zap.NewNop()

	nodeA := newNodeConnection(connA, nodeB_ID, cfg, logger.Named(string(nodeA_ID)))
	nodeB := newNodeConnection(connB, nodeA_ID, cfg, logger.Named(string(nodeB_ID)))

	t.Cleanup(func() {
		nodeA.Close()
		nodeB.Close()
	})
	return nodeA, nodeB
}

// performTestHandshake is a helper to robustly execute the handshake for a pair of nodes.
func performTestHandshake(t *testing.T, nodeA, nodeB *NodeConnection, nodeA_ID, nodeB_ID cluster.NodeID) {
	t.Helper()
	var wg sync.WaitGroup
	wg.Add(2)
	var errA, errB error
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		defer wg.Done()
		errA = nodeA.performHandshake(ctx, nodeA_ID, true)
	}()
	go func() {
		defer wg.Done()
		errB = nodeB.performHandshake(ctx, nodeB_ID, false)
	}()

	wg.Wait()
	if errA != nil {
		t.Fatalf("Initiator handshake for %s failed: %v", nodeA_ID, errA)
	}
	if errB != nil {
		t.Fatalf("Follower handshake for %s failed: %v", nodeB_ID, errB)
	}
}

// TestNodeConnection_Handshake verifies the successful handshake process.
func TestNodeConnection_Handshake(t *testing.T) {
	nodeA, nodeB := setupTestConnections(t, "node-A", "node-B", DefaultNodeConnectionConfig())
	performTestHandshake(t, nodeA, nodeB, "node-A", "node-B")

	if nodeB.RemoteNodeID() != "node-A" {
		t.Errorf("Follower did not correctly identify initiator. Got %q, want %q", nodeB.RemoteNodeID(), "node-A")
	}
}

// TestNodeConnection_HandshakeMismatch verifies that the handshake fails when node IDs don't match.
func TestNodeConnection_HandshakeMismatch(t *testing.T) {
	nodeA, nodeB := setupTestConnections(t, "node-A", "node-Z", DefaultNodeConnectionConfig())
	var errA error
	var wg sync.WaitGroup
	wg.Add(2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go func() {
		defer wg.Done()
		errA = nodeA.performHandshake(ctx, "node-A", true)
	}()
	go func() {
		defer wg.Done()
		// errB is not checked as the initiator is responsible for detecting the mismatch.
		_ = nodeB.performHandshake(ctx, "node-B", false)
	}()
	wg.Wait()

	if errA == nil {
		t.Fatal("Initiator handshake should have failed, but it succeeded.")
	}
	if !strings.Contains(errA.Error(), "node ID mismatch") {
		t.Errorf("Expected error to contain 'node ID mismatch', but got %v", errA)
	}
}

// TestNodeConnection_SendReceive verifies basic message passing functionality.
func TestNodeConnection_SendReceive(t *testing.T) {
	nodeA, nodeB := setupTestConnections(t, "node-A", "node-B", DefaultNodeConnectionConfig())
	performTestHandshake(t, nodeA, nodeB, "node-A", "node-B")
	msgChan := make(chan []byte, 1)

	onMessage := func(nodeID cluster.NodeID, data []byte) {
		copiedData := make([]byte, len(data))
		copy(copiedData, data)
		msgChan <- copiedData
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start both nodes' run loops
	go nodeA.Run(ctx, func(cluster.NodeID, []byte) {})
	go nodeB.Run(ctx, onMessage)

	testMsg := []byte("hello, world!")
	if err := nodeA.Send(testMsg); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	select {
	case receivedMsg := <-msgChan:
		if !bytes.Equal(testMsg, receivedMsg) {
			t.Errorf("Mismatched message. Got %q, want %q", receivedMsg, testMsg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Test timed out waiting for message")
	}
}

// TestNodeConnection_Shutdown verifies that closing one connection terminates the peer's run loop.
func TestNodeConnection_Shutdown(t *testing.T) {
	nodeA, nodeB := setupTestConnections(t, "node-A", "node-B", DefaultNodeConnectionConfig())
	runLoopExited := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		nodeB.Run(ctx, func(cluster.NodeID, []byte) {})
		close(runLoopExited)
	}()

	time.Sleep(50 * time.Millisecond) // Give the run loop a moment to start.
	nodeA.Close()

	select {
	case <-runLoopExited:
		// Success.
	case <-time.After(2 * time.Second):
		t.Fatal("Test timed out waiting for run loop to exit after peer closed.")
	}
}

// TestNodeConnection_SelfClose verifies that calling Close on a connection terminates its own run loop.
func TestNodeConnection_SelfClose(t *testing.T) {
	nodeA, _ := setupTestConnections(t, "node-A", "node-B", DefaultNodeConnectionConfig())
	runLoopExited := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		nodeA.Run(ctx, func(cluster.NodeID, []byte) {})
		close(runLoopExited)
	}()

	time.Sleep(50 * time.Millisecond) // Give the run loop a moment to start.
	nodeA.Close()

	select {
	case <-runLoopExited:
		// Success.
	case <-time.After(2 * time.Second):
		t.Fatal("Test timed out waiting for run loop to exit after Close() was called.")
	}
}

// TestNodeConnection_ZeroLengthMessage tests sending an empty message payload.
func TestNodeConnection_ZeroLengthMessage(t *testing.T) {
	nodeA, nodeB := setupTestConnections(t, "node-A", "node-B", DefaultNodeConnectionConfig())
	performTestHandshake(t, nodeA, nodeB, "node-A", "node-B")
	msgChan := make(chan []byte, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start both nodes' run loops
	go nodeA.Run(ctx, func(cluster.NodeID, []byte) {})
	go nodeB.Run(ctx, func(_ cluster.NodeID, data []byte) {
		msgChan <- data
	})

	if err := nodeA.Send([]byte{}); err != nil {
		t.Fatalf("Send failed for zero-length message: %v", err)
	}

	select {
	case msg := <-msgChan:
		if len(msg) != 0 {
			t.Errorf("Expected a zero-length message, but got one with length %d", len(msg))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Test timed out waiting for zero-length message")
	}
}

// TestNodeConnection_ConcurrentSend verifies sending from multiple goroutines is safe and reliable.
func TestNodeConnection_ConcurrentSend(t *testing.T) {
	cfg := DefaultNodeConnectionConfig()
	cfg.OutboundQueueSize = 512 // Use a bigger queue
	nodeA, nodeB := setupTestConnections(t, "node-A", "node-B", cfg)
	performTestHandshake(t, nodeA, nodeB, "node-A", "node-B")

	const numMessages = 5000
	const numSenders = 20
	var receivedCount int64
	doneChan := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start both nodes' run loops
	go nodeA.Run(ctx, func(cluster.NodeID, []byte) {})
	go nodeB.Run(ctx, func(cluster.NodeID, []byte) {
		if atomic.AddInt64(&receivedCount, 1) == numMessages {
			close(doneChan)
		}
	})

	var sendWg sync.WaitGroup
	sendWg.Add(numSenders)
	for i := 0; i < numSenders; i++ {
		go func(senderID int) {
			defer sendWg.Done()
			for j := 0; j < numMessages/numSenders; j++ {
				msg := []byte(fmt.Sprintf("sender-%d-msg-%d", senderID, j))
				if err := nodeA.Send(msg); err != nil {
					// We might get an error if the connection is closed before we finish sending.
					return
				}
			}
		}(i)
	}

	select {
	case <-doneChan:
		// Success
	case <-time.After(10 * time.Second):
		t.Fatalf("Test timed out waiting for all messages. Received %d out of %d", atomic.LoadInt64(&receivedCount), numMessages)
	}
	sendWg.Wait()
}

// TestNodeConnection_SendWithSlowReceiver_Overflow verifies that send does not block
// and messages are queued correctly when the receiver is slow.
func TestNodeConnection_SendWithSlowReceiver_Overflow(t *testing.T) {
	cfg := DefaultNodeConnectionConfig()
	cfg.OutboundQueueSize = 10 // A small queue to force overflow
	nodeA, nodeB := setupTestConnections(t, "node-A", "node-B", cfg)
	performTestHandshake(t, nodeA, nodeB, "node-A", "node-B")

	const totalMessages = 100
	var receivedCount int64
	doneChan := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start both nodes' run loops
	go nodeA.Run(ctx, func(cluster.NodeID, []byte) {})
	go nodeB.Run(ctx, func(cluster.NodeID, []byte) {
		time.Sleep(5 * time.Millisecond) // Simulate slow processing
		if atomic.AddInt64(&receivedCount, 1) == totalMessages {
			close(doneChan)
		}
	})

	time.Sleep(50 * time.Millisecond) // Give the receiver a moment to start

	// Blast messages much faster than the receiver can handle them.
	// These Send calls should not block.
	for i := 0; i < totalMessages; i++ {
		msg := []byte(fmt.Sprintf("message %d", i))
		if err := nodeA.Send(msg); err != nil {
			t.Fatalf("Send failed unexpectedly: %v", err)
		}
	}

	select {
	case <-doneChan:
		// Success, all messages were drained and received.
	case <-time.After(10 * time.Second):
		t.Fatalf("Test timed out. Received %d of %d messages.", atomic.LoadInt64(&receivedCount), totalMessages)
	}
}

// TestNodeConnection_ExtractPendingMessages verifies that messages from both the
// channel and overflow list are recovered in the correct order.
func TestNodeConnection_ExtractPendingMessages(t *testing.T) {
	cfg := DefaultNodeConnectionConfig()
	cfg.OutboundQueueSize = 5
	// We only need one side of the connection for this test as we don't run the loops.
	conn, _ := net.Pipe()
	nodeA := newNodeConnection(conn, "node-B", cfg, zap.NewNop())
	defer nodeA.Close()

	numChannelMsgs := cfg.OutboundQueueSize
	numOverflowMsgs := 10
	totalMessages := numChannelMsgs + numOverflowMsgs

	// Send messages. The first 5 should go to the channel, the next 10 to overflow.
	for i := 0; i < totalMessages; i++ {
		msg := []byte(fmt.Sprintf("msg-%d", i))
		if err := nodeA.Send(msg); err != nil {
			t.Fatalf("Send returned an unexpected error on message %d: %v", i, err)
		}
	}

	// Now extract them. This should pull from the channel first, then the overflow list.
	pending := nodeA.ExtractPendingMessages()

	if len(pending) != totalMessages {
		t.Fatalf("Expected %d pending messages, but got %d", totalMessages, len(pending))
	}

	// Verify the order is correct.
	for i := 0; i < totalMessages; i++ {
		expectedMsg := []byte(fmt.Sprintf("msg-%d", i))
		if !bytes.Equal(expectedMsg, pending[i]) {
			t.Errorf("Message at index %d is incorrect. Got %q, want %q", i, pending[i], expectedMsg)
		}
	}
}

// TestNodeConnection_ErrorOnSendToClosed verifies Send returns an error on a closed connection.
func TestNodeConnection_ErrorOnSendToClosed(t *testing.T) {
	nodeA, _ := setupTestConnections(t, "node-A", "node-B", DefaultNodeConnectionConfig())
	nodeA.Close()
	time.Sleep(10 * time.Millisecond) // Allow a moment for the close to propagate.

	err := nodeA.Send([]byte("this should fail"))
	if err == nil {
		t.Fatal("Expected an error when sending to a closed connection, but got nil")
	}
	if err != ErrConnectionClosed {
		t.Errorf("Expected error ErrConnectionClosed, but got: %v", err)
	}
}
