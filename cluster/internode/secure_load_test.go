// SPDX-License-Identifier: MPL-2.0
package internode

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

// Exercises the actual TCP/TLS/framing path, including signed node identity and
// cluster-key authentication. This is a bounded two-node test, not a benchmark.
func authenticatedTestManagers(t *testing.T, configure func(*ManagerConfig)) (ConnectionManager, ConnectionManager) {
	t.Helper()
	tlsA, tlsB := managerTestTLSConfigs(t)
	publicA, privateA, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	publicB, privateB, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	secret := make([]byte, 32)
	_, err = rand.Read(secret)
	require.NoError(t, err)
	config := func(self, peer cluster.NodeID, key ed25519.PrivateKey, public ed25519.PublicKey, tls ManagerTLSConfig) ManagerConfig {
		c := DefaultManagerConfig()
		c.LocalNodeID, c.BindAddr, c.AutoPort = self, "127.0.0.1", true
		c.Logger, c.TLS, c.AuthenticationKey, c.SigningKey = zap.NewNop(), tls, secret, key
		c.ResolvePeerKey = func(id cluster.NodeID) (ed25519.PublicKey, bool) { return public, id == peer }
		c.AuthorizePeer = func(id cluster.NodeID, _ net.Addr) bool { return id == peer }
		return c
	}
	aCfg := config("node-a", "node-b", privateA, publicB, tlsA)
	bCfg := config("node-b", "node-a", privateB, publicA, tlsB)
	if configure != nil {
		configure(&aCfg)
		configure(&bCfg)
	}
	return NewConnectionManager(aCfg, nil), NewConnectionManager(bCfg, nil)
}

func TestIntegration_TLSAuthenticatedMixedPayloadOrder(t *testing.T) {
	a, b := authenticatedTestManagers(t, nil)
	const frames = 128
	payload := func(sequence int) []byte {
		size := 64
		if sequence%2 != 0 {
			size = 256 << 10
		}
		data := bytes.Repeat([]byte{byte(sequence)}, size)
		binary.BigEndian.PutUint64(data, uint64(sequence))
		return data
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan struct{})
	failures := make(chan error, 1)
	var count atomic.Int32
	var total atomic.Int64
	require.NoError(t, a.Start(ctx, func(cluster.NodeID, []byte) {}))
	defer func() { require.NoError(t, a.Stop()) }()
	require.NoError(t, b.Start(ctx, func(peer cluster.NodeID, data []byte) {
		sequence := int(count.Add(1)) - 1
		if peer != "node-a" || sequence >= frames || !bytes.Equal(data, payload(sequence)) {
			select {
			case failures <- fmt.Errorf("invalid peer, payload or sequence at %d", sequence):
			default:
			}
		}
		total.Add(int64(len(data)))
		if sequence == frames-1 {
			close(done)
		}
	}))
	defer func() { require.NoError(t, b.Stop()) }()
	a.AddManagedNode("node-b")
	b.AddManagedNode("node-a")
	a.EnsureConnection("node-b", "127.0.0.1", b.GetListenPort())
	require.Eventually(t, func() bool { return len(a.ConnectedNodes()) == 1 && len(b.ConnectedNodes()) == 1 }, 3*time.Second, 10*time.Millisecond)
	began := time.Now()
	sender := a.(ContextConnectionManager)
	for sequence := 0; sequence < frames; sequence++ {
		require.NoError(t, sender.SendToNodeContext(ctx, "node-b", payload(sequence), ClassRaftControl))
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-failures:
		t.Fatal(err)
	default:
	}
	require.Equal(t, int32(frames), count.Load())
	require.Equal(t, int64(frames/2*(64+(256<<10))), total.Load())
	t.Logf("TLS + node identity + cluster key: %d ordered mixed-size frames, %d bytes in %s", frames, total.Load(), time.Since(began))
}

func TestIntegration_TLSBackpressureCancellationAndResume(t *testing.T) {
	const size = 4 << 20
	a, b := authenticatedTestManagers(t, func(c *ManagerConfig) {
		c.OutboundQueueSize = 1
		// This fixture deliberately exercises a single shared capacity without reserves.
		c.OutboundControlPeerEntries, c.OutboundControlPeerBytes = 0, 0
		c.OutboundControlTotalEntries, c.OutboundControlTotalBytes = 0, 0
		c.OutboundPeerBytes = size
		c.OutboundTotalBytes = size
		c.OutboundTotalEntries = 1
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	release := make(chan struct{})
	entered := make(chan struct{})
	received := make(chan int, 64)
	failures := make(chan error, 1)
	var count atomic.Int32
	payload := func(sequence int) []byte {
		data := bytes.Repeat([]byte{byte(sequence)}, size)
		binary.BigEndian.PutUint64(data, uint64(sequence))
		return data
	}
	require.NoError(t, a.Start(ctx, func(cluster.NodeID, []byte) {}))
	defer func() { cancel(); require.NoError(t, a.Stop()) }()
	require.NoError(t, b.Start(ctx, func(peer cluster.NodeID, data []byte) {
		sequence := int(count.Add(1)) - 1
		if peer != "node-a" || !bytes.Equal(data, payload(sequence)) {
			select {
			case failures <- fmt.Errorf("invalid received sequence %d", sequence):
			default:
			}
		}
		received <- sequence
		if sequence == 0 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}))
	defer func() { cancel(); require.NoError(t, b.Stop()) }()
	a.AddManagedNode("node-b")
	b.AddManagedNode("node-a")
	a.EnsureConnection("node-b", "127.0.0.1", b.GetListenPort())
	require.Eventually(t, func() bool { return len(a.ConnectedNodes()) == 1 && len(b.ConnectedNodes()) == 1 }, 3*time.Second, time.Millisecond)
	sender := a.(ContextConnectionManager)
	require.NoError(t, sender.SendToNodeContext(ctx, "node-b", payload(0), ClassRaftRPC))
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	pendingCtx, stopPending := context.WithCancel(ctx)
	defer stopPending()
	var accepted atomic.Int32
	accepted.Store(1)
	done := make(chan error, 1)
	go func() {
		for sequence := 1; sequence < 64; sequence++ {
			if err := sender.SendToNodeContext(pendingCtx, "node-b", payload(sequence), ClassRaftRPC); err != nil {
				done <- err
				return
			}
			accepted.Add(1)
		}
		done <- fmt.Errorf("stalled receiver did not backpressure within 256 MiB")
	}()
	budget := a.(*manager).nodeStates.reliable
	require.Eventually(t, func() bool {
		budget.mu.Lock()
		defer budget.mu.Unlock()
		return budget.waiters > 0
	}, 3*time.Second, time.Millisecond)
	stopPending()
	require.ErrorIs(t, <-done, context.Canceled)
	budget.mu.Lock()
	retainedBytes, retainedEntries := budget.bytes, budget.entries
	budget.mu.Unlock()
	require.LessOrEqual(t, retainedBytes, uint64(size))
	require.LessOrEqual(t, retainedEntries, uint64(1))
	close(release)
	// A fresh send uses the canceled call's sequence: refused work cannot appear.
	finalSequence := int(accepted.Load())
	require.NoError(t, sender.SendToNodeContext(ctx, "node-b", payload(finalSequence), ClassRaftRPC))
	for sequence := 0; sequence <= finalSequence; sequence++ {
		select {
		case got := <-received:
			require.Equal(t, sequence, got)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	select {
	case err := <-failures:
		t.Fatal(err)
	default:
	}
	require.Eventually(t, func() bool {
		budget.mu.Lock()
		defer budget.mu.Unlock()
		return budget.bytes == 0 && budget.entries == 0
	}, time.Second, time.Millisecond)
}

// A stopped application consumer fills the actual TLS writer and its admission
// budget. Coordination still admits, but delivery waits for the shared receiver
// to resume: this tests admission isolation, not wire/receive preemption.
func TestIntegration_TLSControlAdmissionUnderApplicationPressure(t *testing.T) {
	const size = 64 << 10
	a, b := authenticatedTestManagers(t, func(c *ManagerConfig) {
		c.OutboundQueueSize, c.OutboundPeerBytes = 64, 4<<20
		c.OutboundTotalEntries, c.OutboundTotalBytes = 128, 8<<20
		c.OutboundControlPeerEntries, c.OutboundControlPeerBytes = 4, size
		c.OutboundControlTotalEntries, c.OutboundControlTotalBytes = 8, 2*size
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	release := make(chan struct{})
	entered := make(chan struct{})
	control := make(chan []byte, 1)
	failures := make(chan error, 1)
	var received atomic.Int32
	payload := func(sequence int) []byte {
		data := bytes.Repeat([]byte{byte(sequence)}, size)
		binary.BigEndian.PutUint64(data, uint64(sequence))
		return data
	}
	require.True(t, b.RegisterClassReceiver(ClassRaftRPC, func(peer cluster.NodeID, data []byte) {
		if peer != "node-a" {
			select {
			case failures <- fmt.Errorf("wrong control peer %s", peer):
			default:
			}
		}
		// Inbound storage belongs to the callback; retain only a copy.
		control <- bytes.Clone(data)
	}))
	require.NoError(t, a.Start(ctx, func(cluster.NodeID, []byte) {}))
	defer func() { cancel(); require.NoError(t, a.Stop()) }()
	require.NoError(t, b.Start(ctx, func(peer cluster.NodeID, data []byte) {
		sequence := int(received.Load())
		if peer != "node-a" || !bytes.Equal(data, payload(sequence)) {
			select {
			case failures <- fmt.Errorf("invalid application frame %d", sequence):
			default:
			}
		}
		received.Add(1)
		if sequence == 0 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}))
	defer func() { cancel(); require.NoError(t, b.Stop()) }()
	a.AddManagedNode("node-b")
	b.AddManagedNode("node-a")
	a.EnsureConnection("node-b", "127.0.0.1", b.GetListenPort())
	require.Eventually(t, func() bool { return len(a.ConnectedNodes()) == 1 && len(b.ConnectedNodes()) == 1 }, 3*time.Second, time.Millisecond)
	sender := a.(ContextConnectionManager)
	require.NoError(t, sender.SendToNodeContext(ctx, "node-b", payload(0), ClassPGBroadcast))
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	pendingCtx, cancelPending := context.WithCancel(ctx)
	defer cancelPending()
	var accepted atomic.Int32
	accepted.Store(1)
	done := make(chan error, 1)
	go func() {
		for sequence := 1; sequence < 1024; sequence++ {
			if err := sender.SendToNodeContext(pendingCtx, "node-b", payload(sequence), ClassPGBroadcast); err != nil {
				done <- err
				return
			}
			accepted.Add(1)
		}
		done <- fmt.Errorf("stalled application did not backpressure within 64 MiB")
	}()
	budget := a.(*manager).nodeStates.reliable
	require.Eventually(t, func() bool {
		budget.mu.Lock()
		defer budget.mu.Unlock()
		return budget.waiters > 0
	}, 3*time.Second, time.Millisecond)
	cancelPending()
	require.ErrorIs(t, <-done, context.Canceled)
	began := time.Now()
	require.NoError(t, a.SendToNode("node-b", []byte("control"), ClassRaftRPC))
	t.Logf("control admission under actual TLS application backpressure: %s; %d accepted application frames", time.Since(began), accepted.Load())
	budget.mu.Lock()
	entries, retained := budget.entries, budget.bytes
	budget.mu.Unlock()
	require.LessOrEqual(t, entries, uint64(128))
	require.LessOrEqual(t, retained, uint64(8<<20))
	close(release)
	select {
	case got := <-control:
		require.Equal(t, []byte("control"), got)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Eventually(t, func() bool { return received.Load() == accepted.Load() }, 3*time.Second, time.Millisecond)
	select {
	case err := <-failures:
		t.Fatal(err)
	default:
	}
	require.Eventually(t, func() bool {
		budget.mu.Lock()
		defer budget.mu.Unlock()
		return budget.entries == 0 && budget.bytes == 0 && budget.ordinaryEntries == 0 && budget.ordinaryBytes == 0
	}, time.Second, time.Millisecond)
}
