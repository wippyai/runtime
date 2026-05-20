// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestMux_ClassRoundTripPerClass sends one frame on every Class across a
// loopback NodeConnection pair and asserts both the payload and the
// decoded Class arrive unchanged on the receiver. Without the wire-level
// class byte the receiver would only see opaque payloads and could not
// dispatch to per-class handlers.
func TestMux_ClassRoundTripPerClass(t *testing.T) {
	a, b := newTestConnectionPair(t, "node-A", "node-B")

	type seen struct {
		data  []byte
		class Class
	}
	got := make(chan seen, numClasses)

	go func() { _ = a.Run(func(_ Class, _ []byte) {}) }()
	go func() {
		_ = b.Run(func(class Class, msg []byte) {
			cp := make([]byte, len(msg))
			copy(cp, msg)
			got <- seen{class: class, data: cp}
		})
	}()

	classes := []Class{ClassRaftControl, ClassGossip, ClassPGBroadcast, ClassRaftMesh}
	for _, c := range classes {
		payload := []byte("payload-" + c.String())
		require.NoError(t, a.Send(payload, c))
	}

	seenByClass := map[Class][]byte{}
	deadline := time.After(2 * time.Second)
	for i := 0; i < len(classes); i++ {
		select {
		case s := <-got:
			seenByClass[s.class] = s.data
		case <-deadline:
			t.Fatalf("only received %d/%d frames: %v", i, len(classes), seenByClass)
		}
	}

	for _, c := range classes {
		require.Equalf(t, []byte("payload-"+c.String()), seenByClass[c],
			"class %s payload mismatch", c)
	}
}

// TestMux_ConcurrentSendersDoNotInterleave hammers the connection with
// concurrent senders on every Class and asserts no payload is corrupted
// (which would indicate frame bytes from different sends interleaving on
// the wire). Bounds the test by sender-class so a single counter per
// class confirms arrival rather than per-message equality on a noisy
// channel.
func TestMux_ConcurrentSendersDoNotInterleave(t *testing.T) {
	a, b := newTestConnectionPair(t, "node-A", "node-B")

	const perClass = 500
	classes := []Class{ClassRaftControl, ClassGossip, ClassPGBroadcast, ClassRaftMesh}

	var perClassCount [numClasses]atomic.Int64
	done := make(chan struct{})
	var once sync.Once

	expectedTotal := int64(perClass * len(classes))

	go func() { _ = a.Run(func(_ Class, _ []byte) {}) }()
	go func() {
		_ = b.Run(func(class Class, msg []byte) {
			// Payload format: [class byte][seq-le-uint32]. If two
			// concurrent senders ever interleaved, the recovered class
			// byte would not match the wire class for at least one
			// frame.
			if int(class) >= numClasses || len(msg) == 0 || Class(msg[0]) != class {
				t.Errorf("interleave: wire class %d payload byte %d", class, msg[0])
				once.Do(func() { close(done) })
				return
			}
			n := perClassCount[class].Add(1)
			var total int64
			for i := range perClassCount {
				total += perClassCount[i].Load()
			}
			if total == expectedTotal {
				once.Do(func() { close(done) })
			}
			_ = n
		})
	}()

	var sendWg sync.WaitGroup
	for _, c := range classes {
		sendWg.Add(1)
		go func(class Class) {
			defer sendWg.Done()
			for i := uint32(0); i < perClass; i++ {
				// Allocate fresh per send — Send queues the slice without
				// copying, so reusing one buffer would race with the
				// writeLoop's bufio flush of an earlier frame.
				payload := []byte{byte(class), byte(i), byte(i >> 8), 0, 0}
				require.NoError(t, a.Send(payload, class))
			}
		}(c)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		counts := make([]int64, numClasses)
		for i := range counts {
			counts[i] = perClassCount[i].Load()
		}
		t.Fatalf("did not receive %d frames in time; counts=%v",
			expectedTotal, counts)
	}
	sendWg.Wait()
}

// TestMux_UnknownClassOnWireSurfaceProtocolError corrupts the class byte
// in a frame and asserts the read loop terminates with ExitProtocolError
// instead of silently delivering the payload to the wrong receiver. The
// guard exists so a forward-incompatible peer cannot inject frames that
// look opaque to today's handler set.
func TestMux_UnknownClassOnWireSurfaceProtocolError(t *testing.T) {
	mockA, mockB := newMockConnPair()
	cfg := DefaultNodeConnectionConfig()

	nodeB := newNodeConnection(mockB, "node-A", cfg, zap.NewNop())
	t.Cleanup(func() { nodeB.Close() })

	runErr := make(chan *ConnectionError, 1)
	go func() { runErr <- nodeB.Run(func(_ Class, _ []byte) {}) }()

	// Hand-craft a frame with a class byte outside the legal range.
	frame := []byte{
		protocolVersion,
		0x7f, // invalid class
		0x00, 0x00, 0x00, 0x00,
	}
	_, _ = mockA.Write(frame)

	select {
	case err := <-runErr:
		require.NotNil(t, err)
		require.Equal(t, ExitProtocolError, err.Reason)
	case <-time.After(2 * time.Second):
		t.Fatal("expected protocol error after invalid class byte")
	}
}

// TestMux_RegisterClassReceiverRoutesPerClass verifies the ConnectionManager
// dispatch table: when a per-class receiver is registered, frames of that
// class skip the default onMessage callback and arrive at the receiver
// instead. Frames of other classes continue to flow through onMessage.
func TestMux_RegisterClassReceiverRoutesPerClass(t *testing.T) {
	cfg := DefaultManagerConfig()
	cfg.Logger = zap.NewNop()
	cfg.LocalNodeID = "node-A"
	cfg.AutoPort = true
	cfg.BindAddr = "127.0.0.1"

	mgrA := NewConnectionManager(cfg, nil)
	defer func() { _ = mgrA.Stop() }()

	defaultDelivered := make(chan []byte, 4)
	require.NoError(t, mgrA.Start(t.Context(), func(_ string, data []byte) {
		cp := make([]byte, len(data))
		copy(cp, data)
		defaultDelivered <- cp
	}))

	raftDelivered := make(chan []byte, 4)
	ok := mgrA.RegisterClassReceiver(ClassRaftMesh, func(_ string, data []byte) {
		cp := make([]byte, len(data))
		copy(cp, data)
		raftDelivered <- cp
	})
	require.True(t, ok)
	require.False(t, mgrA.RegisterClassReceiver(ClassRaftMesh,
		func(_ string, _ []byte) {}), "second registration must fail")

	// Spin up the peer leg manually so we can stay inside the package.
	cfgB := DefaultManagerConfig()
	cfgB.Logger = zap.NewNop()
	cfgB.LocalNodeID = "node-B"
	cfgB.BindAddr = "127.0.0.1"
	cfgB.AutoPort = true

	mgrB := NewConnectionManager(cfgB, nil)
	defer func() { _ = mgrB.Stop() }()
	require.NoError(t, mgrB.Start(t.Context(), func(_ string, _ []byte) {}))

	mgrA.AddManagedNode("node-B")
	mgrB.AddManagedNode("node-A")

	mgrA.EnsureConnection("node-B", "127.0.0.1", mgrB.GetListenPort())
	mgrB.EnsureConnection("node-A", "127.0.0.1", mgrA.GetListenPort())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if isConnected(mgrA, "node-B") && isConnected(mgrB, "node-A") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.True(t, isConnected(mgrA, "node-B"))
	require.True(t, isConnected(mgrB, "node-A"))

	require.NoError(t, mgrB.SendToNode("node-A", []byte("default-payload"), ClassPGBroadcast))
	require.NoError(t, mgrB.SendToNode("node-A", []byte("raft-payload"), ClassRaftMesh))

	select {
	case got := <-defaultDelivered:
		require.Equal(t, []byte("default-payload"), got)
	case <-time.After(2 * time.Second):
		t.Fatal("default onMessage never fired")
	}

	select {
	case got := <-raftDelivered:
		require.Equal(t, []byte("raft-payload"), got)
	case <-time.After(2 * time.Second):
		t.Fatal("ClassRaftMesh receiver never fired")
	}
}

func isConnected(mgr ConnectionManager, peer string) bool {
	for _, c := range mgr.ConnectedNodes() {
		if c == peer {
			return true
		}
	}
	return false
}
