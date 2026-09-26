// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

// trafficClasses are the classes a traffic test cycles through. Gossip is
// included to verify it never duplicates; it may be lost.
var trafficClasses = [...]Class{ClassRaftControl, ClassPGBroadcast, ClassRaftRPC, ClassGossip}

// trafficRecorder records every frame a node receives from its peer. Frames
// carry [class u8][counter u64] so ordering is checked per class.
type trafficRecorder struct {
	got map[Class][]uint64
	mu  sync.Mutex
}

func newTrafficRecorder() *trafficRecorder {
	return &trafficRecorder{got: make(map[Class][]uint64)}
}

func (r *trafficRecorder) record(data []byte) {
	if len(data) != 9 {
		panic(fmt.Sprintf("unexpected frame length %d", len(data)))
	}
	r.mu.Lock()
	r.got[Class(data[0])] = append(r.got[Class(data[0])], binary.BigEndian.Uint64(data[1:]))
	r.mu.Unlock()
}

func (r *trafficRecorder) count(class Class) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got[class])
}

// requireExactlyOnceInOrder checks that every reliable class delivered
// 0..n-1 exactly once in order, and that gossip never duplicated or
// reordered.
func (r *trafficRecorder) requireExactlyOnceInOrder(t *testing.T, sent map[Class]uint64) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for class, n := range sent {
		got := r.got[class]
		if class == ClassGossip {
			for i := 1; i < len(got); i++ {
				require.Greater(t, got[i], got[i-1], "gossip duplicated or reordered at %d", i)
			}
			continue
		}
		require.Len(t, got, int(n), "class %s: delivered %d of %d", class, len(got), n)
		for i, v := range got {
			require.Equal(t, uint64(i), v, "class %s: frame %d out of order or duplicated", class, i)
		}
	}
}

func trafficFrame(class Class, counter uint64) []byte {
	b := make([]byte, 9)
	b[0] = byte(class)
	binary.BigEndian.PutUint64(b[1:], counter)
	return b
}

type trafficNode struct {
	manager *manager
	rec     *trafficRecorder
	id      cluster.NodeID
}

// startTrafficPair starts two plain-TCP managers that manage each other and
// record everything they receive.
func startTrafficPair(t *testing.T) (a, b *trafficNode) {
	t.Helper()
	start := func(self, peer cluster.NodeID) *trafficNode {
		cfg := insecureManagerConfig()
		cfg.LocalNodeID = self
		cfg.BindAddr = "127.0.0.1"
		cfg.BindPort = 0
		cfg.Logger = zap.NewNop()
		cfg.InitialRetryDelay = time.Millisecond
		cfg.MaxRetryDelay = 10 * time.Millisecond
		node := &trafficNode{id: self, rec: newTrafficRecorder()}
		node.manager = NewConnectionManager(cfg, nil).(*manager)
		require.NoError(t, startManager(node.manager, func(from cluster.NodeID, data []byte) {
			if from == peer {
				node.rec.record(data)
			}
		}))
		t.Cleanup(func() { require.NoError(t, node.manager.Stop()) })
		node.manager.AddManagedNode(peer)
		return node
	}
	return start("node-a", "node-b"), start("node-b", "node-a")
}

func requireLinked(t *testing.T, a, b *trafficNode) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, as := a.manager.nodeStates.GetNodeConnection(b.id)
		_, bs := b.manager.nodeStates.GetNodeConnection(a.id)
		return as == StateConnected && bs == StateConnected
	}, 5*time.Second, time.Millisecond)
}

// sendTraffic sends n frames per class from node to peer and returns the
// per-class counts sent.
func sendTraffic(t *testing.T, from *trafficNode, to cluster.NodeID, n int, between func(i int)) map[Class]uint64 {
	t.Helper()
	sent := make(map[Class]uint64)
	for i := 0; i < n; i++ {
		for _, class := range trafficClasses {
			err := from.manager.SendToNode(to, trafficFrame(class, sent[class]), class)
			if class == ClassGossip && err != nil {
				require.ErrorIs(t, err, ErrQueueFull)
				continue
			}
			require.NoError(t, err)
			sent[class]++
		}
		if between != nil {
			between(i)
		}
	}
	return sent
}

func waitReliableDelivered(t *testing.T, rec *trafficRecorder, sent map[Class]uint64) {
	t.Helper()
	require.Eventually(t, func() bool {
		for class, n := range sent {
			if class != ClassGossip && rec.count(class) < int(n) {
				return false
			}
		}
		return true
	}, 20*time.Second, time.Millisecond)
	// Allow a straggler duplicate to surface before checking.
	time.Sleep(50 * time.Millisecond)
}

// abortLink kills the socket under the node's current connection to peer.
func abortLink(m *manager, peer cluster.NodeID) {
	if conn, _ := m.nodeStates.GetNodeConnection(peer); conn != nil {
		abortConnection(conn.conn)
	}
}

// Sustained traffic in both directions while the socket is repeatedly killed:
// every reliable frame arrives exactly once and in order.
func TestReliableLinkSurvivesRepeatedAborts(t *testing.T) {
	a, b := startTrafficPair(t)
	a.manager.EnsureConnection(b.id, "127.0.0.1", b.manager.GetListenPort())
	b.manager.EnsureConnection(a.id, "127.0.0.1", a.manager.GetListenPort())
	requireLinked(t, a, b)

	const perClass = 4000
	var sentBA map[Class]uint64
	done := make(chan struct{})
	go func() {
		defer close(done)
		sentBA = sendTraffic(t, b, a.id, perClass, nil)
	}()
	sentAB := sendTraffic(t, a, b.id, perClass, func(i int) {
		if i%(perClass/20) == perClass/40 {
			if i%2 == 0 {
				abortLink(a.manager, b.id)
			} else {
				abortLink(b.manager, a.id)
			}
		}
	})
	<-done

	waitReliableDelivered(t, b.rec, sentAB)
	waitReliableDelivered(t, a.rec, sentBA)
	b.rec.requireExactlyOnceInOrder(t, sentAB)
	a.rec.requireExactlyOnceInOrder(t, sentBA)
}

// Both sides dial at once while traffic flows; the swap to the preferred
// socket is lossless and duplicate-free.
func TestReliableLinkSimultaneousDialUnderTraffic(t *testing.T) {
	for range 5 {
		func() {
			a, b := startTrafficPair(t)
			const perClass = 2000
			var sentBA map[Class]uint64
			done := make(chan struct{})
			go func() {
				defer close(done)
				sentBA = sendTraffic(t, b, a.id, perClass, nil)
			}()
			start := make(chan struct{})
			go func() { <-start; a.manager.EnsureConnection(b.id, "127.0.0.1", b.manager.GetListenPort()) }()
			go func() { <-start; b.manager.EnsureConnection(a.id, "127.0.0.1", a.manager.GetListenPort()) }()
			sentAB := sendTraffic(t, a, b.id, perClass, func(i int) {
				if i == perClass/10 {
					close(start)
				}
			})
			<-done
			waitReliableDelivered(t, b.rec, sentAB)
			waitReliableDelivered(t, a.rec, sentBA)
			b.rec.requireExactlyOnceInOrder(t, sentAB)
			a.rec.requireExactlyOnceInOrder(t, sentBA)
		}()
	}
}

// startManager isolates the Start signature for the traffic tests.
func startManager(m *manager, onMessage func(cluster.NodeID, []byte)) error {
	return m.Start(context.Background(), onMessage, ignoreSessionEnd)
}

// Frames of increasing size sent right after a one-way dial connects arrive
// exactly once and in order, and nothing ends the session, including when
// session agreement is bounded far below the time the frames take.
func TestIncreasingFramesAfterConnectEndNoSession(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sizes   []int
		timeout time.Duration
	}{
		{"default agreement bound", []int{1 << 10, 64 << 10, 256 << 10, 1 << 20}, 5 * time.Second},
		{"agreement bound below transfer time", []int{1 << 10, 8 << 20, 8 << 20, 8 << 20}, 20 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ends atomic.Int32
			var mu sync.Mutex
			var got [][]byte
			start := func(self cluster.NodeID, onMessage func(cluster.NodeID, []byte)) *manager {
				cfg := insecureManagerConfig()
				cfg.LocalNodeID = self
				cfg.BindAddr = "127.0.0.1"
				cfg.BindPort = 0
				cfg.Logger = zap.NewNop()
				cfg.MaxMessageSize = 32 << 20
				cfg.HandshakeTimeout = tc.timeout
				m := NewConnectionManager(cfg, nil).(*manager)
				require.NoError(t, m.Start(context.Background(), onMessage, func(cluster.NodeID) { ends.Add(1) }))
				t.Cleanup(func() { require.NoError(t, m.Stop()) })
				return m
			}
			sender := start("node-1", func(cluster.NodeID, []byte) {})
			receiver := start("node-2", func(_ cluster.NodeID, data []byte) {
				mu.Lock()
				got = append(got, data)
				mu.Unlock()
			})
			sender.AddManagedNode("node-2")
			receiver.AddManagedNode("node-1")
			sender.EnsureConnection("node-2", "127.0.0.1", receiver.GetListenPort())
			require.Eventually(t, func() bool { return len(sender.ConnectedNodes()) > 0 }, 5*time.Second, time.Millisecond)

			for i, size := range tc.sizes {
				msg := make([]byte, size)
				msg[0] = byte(i)
				require.NoError(t, sender.SendToNode("node-2", msg, ClassRaftControl))
			}
			require.Eventually(t, func() bool {
				mu.Lock()
				defer mu.Unlock()
				return len(got) >= len(tc.sizes)
			}, 20*time.Second, time.Millisecond)
			time.Sleep(50 * time.Millisecond)
			mu.Lock()
			defer mu.Unlock()
			require.Len(t, got, len(tc.sizes))
			for i, data := range got {
				require.Len(t, data, tc.sizes[i])
				require.Equal(t, byte(i), data[0])
			}
			require.Zero(t, ends.Load(), "a session ended in a plain two-node send")
		})
	}
}
