package internode

//
//import (
//	"bytes"
//	"context"
//	"fmt"
//	"net"
//	"strings"
//	"sync"
//	"testing"
//	"time"
//
//	"github.com/ponyruntime/pony/api/cluster"
//	"go.uber.org/zap"
//)
//
//// setupTestConnections creates a pair of connected NodeConnection instances.
//func setupTestConnections(t *testing.T, nodeA_ID, nodeB_ID cluster.NodeID) (*NodeConnection, *NodeConnection) {
//	t.Helper()
//	connA, connB := net.Pipe()
//	cfg := DefaultNodeConnectionConfig()
//	logger := zap.NewNop()
//
//	// Use the new NodeConnectionConfig-based constructor
//	nodeA := newNodeConnection(connA, nodeB_ID, cfg, logger.Named(nodeA_ID))
//	nodeB := newNodeConnection(connB, nodeA_ID, cfg, logger.Named(nodeB_ID))
//
//	t.Cleanup(func() {
//		nodeA.Close()
//		nodeB.Close()
//	})
//	return nodeA, nodeB
//}
//
//// performTestHandshake is a helper to robustly execute the handshake for a pair of nodes.
//func performTestHandshake(t *testing.T, nodeA, nodeB *NodeConnection, nodeA_ID, nodeB_ID cluster.NodeID) {
//	t.Helper()
//	var wg sync.WaitGroup
//	wg.Add(2)
//	var errA, errB error
//	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
//	defer cancel()
//
//	go func() {
//		defer wg.Done()
//		errA = nodeA.performHandshake(ctx, nodeA_ID, true)
//	}()
//	go func() {
//		defer wg.Done()
//		errB = nodeB.performHandshake(ctx, nodeB_ID, false)
//	}()
//
//	wg.Wait()
//	if errA != nil {
//		t.Fatalf("Initiator handshake for %s failed: %v", nodeA_ID, errA)
//	}
//	if errB != nil {
//		t.Fatalf("Follower handshake for %s failed: %v", nodeB_ID, errB)
//	}
//}
//
//// TestNodeConnection_Handshake verifies the successful handshake process.
//func TestNodeConnection_Handshake(t *testing.T) {
//	nodeA, nodeB := setupTestConnections(t, "node-A", "node-B")
//	performTestHandshake(t, nodeA, nodeB, "node-A", "node-B")
//	if nodeB.RemoteNodeID() != "node-A" {
//		t.Errorf("Follower did not correctly identify initiator. Got %q, want %q", nodeB.RemoteNodeID(), "node-A")
//	}
//}
//
//// TestNodeConnection_HandshakeMismatch verifies that the handshake fails when node IDs don't match.
//func TestNodeConnection_HandshakeMismatch(t *testing.T) {
//	nodeA, nodeB := setupTestConnections(t, "node-A", "node-Z")
//	var errA error
//	var wg sync.WaitGroup
//	wg.Add(2)
//	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
//	defer cancel()
//
//	go func() {
//		defer wg.Done()
//		errA = nodeA.performHandshake(ctx, "node-A", true)
//	}()
//	go func() {
//		defer wg.Done()
//		// errB is not checked as the initiator is responsible for detecting the mismatch.
//		_ = nodeB.performHandshake(ctx, "node-B", false)
//	}()
//	wg.Wait()
//
//	if errA == nil {
//		t.Fatal("Initiator handshake should have failed, but it succeeded.")
//	}
//	if !strings.Contains(errA.Error(), "node ID mismatch") {
//		t.Errorf("Expected error to contain 'node ID mismatch', but got %v", errA)
//	}
//}
//
//// TestNodeConnection_SendReceive verifies basic message passing functionality.
//func TestNodeConnection_SendReceive(t *testing.T) {
//	nodeA, nodeB := setupTestConnections(t, "node-A", "node-B")
//	performTestHandshake(t, nodeA, nodeB, "node-A", "node-B")
//	msgChan := make(chan []byte, 1)
//
//	onMessage := func(nodeID cluster.NodeID, data []byte) {
//		copiedData := make([]byte, len(data))
//		copy(copiedData, data)
//		msgChan <- copiedData
//	}
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	go nodeB.Run(ctx, onMessage)
//	// Give the receiver a moment to start its read loop.
//	time.Sleep(20 * time.Millisecond)
//
//	testMsg := []byte("hello, world!")
//	if err := nodeA.Send(testMsg); err != nil {
//		t.Fatalf("Send failed: %v", err)
//	}
//
//	select {
//	case receivedMsg := <-msgChan:
//		if !bytes.Equal(testMsg, receivedMsg) {
//			t.Errorf("Mismatched message. Got %q, want %q", receivedMsg, testMsg)
//		}
//	case <-time.After(2 * time.Second):
//		t.Fatal("Test timed out waiting for message")
//	}
//}
//
//// TestNodeConnection_Shutdown verifies that closing one connection terminates the peer's run loop.
//func TestNodeConnection_Shutdown(t *testing.T) {
//	nodeA, nodeB := setupTestConnections(t, "node-A", "node-B")
//	runLoopExited := make(chan struct{})
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//
//	go func() {
//		// Run's lifecycle is now controlled by the provided context.
//		nodeB.Run(ctx, func(cluster.NodeID, []byte) {})
//		close(runLoopExited)
//	}()
//
//	// Give the run loop a moment to start.
//	time.Sleep(50 * time.Millisecond)
//
//	// Closing nodeA will cause nodeB's read loop to get an EOF, which
//	// triggers nodeB.Close(), which cancels its context, causing Run() to return.
//	nodeA.Close()
//
//	select {
//	case <-runLoopExited:
//		// Success.
//	case <-time.After(2 * time.Second):
//		t.Fatal("Test timed out waiting for run loop to exit after peer closed.")
//	}
//}
//
//// TestNodeConnection_SelfClose verifies that calling Close on a connection terminates its own run loop.
//func TestNodeConnection_SelfClose(t *testing.T) {
//	nodeA, _ := setupTestConnections(t, "node-A", "node-B")
//	runLoopExited := make(chan struct{})
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//
//	go func() {
//		nodeA.Run(ctx, func(cluster.NodeID, []byte) {})
//		close(runLoopExited)
//	}()
//
//	// Give the run loop a moment to start before asking it to close itself.
//	time.Sleep(50 * time.Millisecond)
//	nodeA.Close()
//
//	select {
//	case <-runLoopExited:
//		// Success.
//	case <-time.After(2 * time.Second):
//		t.Fatal("Test timed out waiting for run loop to exit after Close() was called.")
//	}
//}
//
//// TestNodeConnection_ZeroLengthMessage tests sending an empty message payload.
//func TestNodeConnection_ZeroLengthMessage(t *testing.T) {
//	nodeA, nodeB := setupTestConnections(t, "node-A", "node-B")
//	performTestHandshake(t, nodeA, nodeB, "node-A", "node-B")
//	msgChan := make(chan []byte, 1)
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//
//	go nodeB.Run(ctx, func(_ cluster.NodeID, data []byte) {
//		copiedData := make([]byte, len(data))
//		copy(copiedData, data)
//		msgChan <- copiedData
//	})
//	// Give the receiver a moment to start its read loop.
//	time.Sleep(20 * time.Millisecond)
//
//	if err := nodeA.Send([]byte{}); err != nil {
//		t.Fatalf("Send failed for zero-length message: %v", err)
//	}
//
//	select {
//	case msg := <-msgChan:
//		if len(msg) != 0 {
//			t.Errorf("Expected a zero-length message, but got one with length %d", len(msg))
//		}
//	case <-time.After(2 * time.Second):
//		t.Fatal("Test timed out waiting for zero-length message")
//	}
//}
//
//// TestNodeConnection_ConcurrentSend verifies sending from multiple goroutines is safe and reliable.
//func TestNodeConnection_ConcurrentSend(t *testing.T) {
//	// Create connections with a larger queue size for this test
//	connA, connB := net.Pipe()
//	cfg := DefaultNodeConnectionConfig()
//	cfg.OutboundQueueSize = 2000 // Increase queue size for concurrent test
//	logger := zap.NewNop()
//
//	nodeA := newNodeConnection(connA, "node-B", cfg, logger.Named("node-A"))
//	nodeB := newNodeConnection(connB, "node-A", cfg, logger.Named("node-B"))
//
//	t.Cleanup(func() {
//		nodeA.Close()
//		nodeB.Close()
//	})
//
//	performTestHandshake(t, nodeA, nodeB, "node-A", "node-B")
//	const numMessages = 1000
//	const numSenders = 10
//	var receiveWg sync.WaitGroup
//	receiveWg
