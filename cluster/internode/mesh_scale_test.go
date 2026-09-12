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
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

// Explicit opt-in: opens 4,950 TLS connections. These are 100 independent
// transport managers in one OS process, not 100 Raft voters or OS processes.
func TestIntegration_TLSHundredNodeMesh(t *testing.T) {
	if os.Getenv("WIPPY_MESH_STRESS") != "1" {
		t.Skip("set WIPPY_MESH_STRESS=1")
	}
	const nodes, frames = 100, 128
	tlsFiles, _ := managerTestTLSConfigs(t)
	ids := make([]cluster.NodeID, nodes)
	private := make([]ed25519.PrivateKey, nodes)
	public := make(map[cluster.NodeID]ed25519.PublicKey, nodes)
	for i := range nodes {
		ids[i] = fmt.Sprintf("scale-%03d", i)
		pub, key, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		public[ids[i]], private[i] = pub, key
	}
	secret := make([]byte, 32)
	_, err := rand.Read(secret)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	managers := make([]ConnectionManager, 0, nodes)
	defer func() {
		cancel()
		var stops sync.WaitGroup
		for _, manager := range managers {
			stops.Go(func() {
				if err := manager.Stop(); err != nil {
					t.Error(err)
				}
			})
		}
		stops.Wait()
	}()
	failures := make(chan error, nodes)
	report := func(err error) {
		select {
		case failures <- err:
		default:
		}
	}
	counts := make([]atomic.Int32, nodes)
	completions := make([]chan struct{}, nodes)
	latencies := make([][]time.Duration, nodes)
	for i := range nodes {
		completions[i] = make(chan struct{})
		latencies[i] = make([]time.Duration, 0, frames)
		cfg := DefaultManagerConfig()
		cfg.LocalNodeID, cfg.BindAddr, cfg.Logger = ids[i], "127.0.0.1", zap.NewNop()
		cfg.TLS, cfg.AuthenticationKey, cfg.SigningKey = tlsFiles, secret, private[i]
		cfg.OutboundQueueSize, cfg.OutboundPeerBytes = 16, 1<<20
		cfg.OutboundTotalEntries, cfg.OutboundTotalBytes = 256, 16<<20
		cfg.OutboundControlPeerEntries, cfg.OutboundControlPeerBytes = 1, 64<<10
		cfg.OutboundControlTotalEntries, cfg.OutboundControlTotalBytes = 16, 1<<20
		cfg.ResolvePeerKey = func(id cluster.NodeID) (ed25519.PublicKey, bool) { key, ok := public[id]; return key, ok }
		cfg.AuthorizePeer = func(id cluster.NodeID, _ net.Addr) bool { _, ok := public[id]; return ok }
		manager := NewConnectionManager(cfg, nil)
		managers = append(managers, manager)
		require.NoError(t, manager.Start(ctx, func(peer cluster.NodeID, data []byte) {
			receivedAt := time.Now()
			sequence := int(counts[i].Add(1)) - 1
			expectedSize := 64
			if sequence%2 != 0 {
				expectedSize = 64 << 10
			}
			valid := peer == ids[(i+nodes-1)%nodes] && sequence < frames && len(data) == expectedSize
			if valid {
				valid = binary.BigEndian.Uint64(data) == uint64(sequence) && bytes.Count(data[16:], []byte{byte(sequence)}) == expectedSize-16
				latencies[i] = append(latencies[i], receivedAt.Sub(time.Unix(0, int64(binary.BigEndian.Uint64(data[8:])))))
			}
			if !valid {
				report(fmt.Errorf("node %d invalid peer/order/payload at %d", i, sequence))
			}
			if sequence == frames-1 {
				close(completions[i])
			}
		}))
	}
	for i, manager := range managers {
		for j := range nodes {
			if i != j {
				manager.AddManagedNode(ids[j])
			}
		}
	}
	began := time.Now()
	for i, manager := range managers {
		for j := i + 1; j < nodes; j++ {
			manager.EnsureConnection(ids[j], "127.0.0.1", managers[j].GetListenPort())
		}
	}
	require.Eventually(t, func() bool {
		for _, manager := range managers {
			if len(manager.ConnectedNodes()) != nodes-1 {
				return false
			}
		}
		return true
	}, 45*time.Second, 50*time.Millisecond)
	t.Logf("connected: %d nodes, %d TLS connections in %s", nodes, nodes*(nodes-1)/2, time.Since(began))
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	goroutines := runtime.NumGoroutine()
	select {
	case <-time.After(30 * time.Second):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	runtime.ReadMemStats(&after)
	t.Logf("idle 30s: goroutines %d -> %d; heap allocated %d -> %d bytes (process total, no forced GC)", goroutines, runtime.NumGoroutine(), before.HeapAlloc, after.HeapAlloc)
	runtime.GC()
	var live runtime.MemStats
	runtime.ReadMemStats(&live)
	t.Logf("idle heap after explicit GC: %d bytes, %d objects", live.HeapAlloc, live.HeapObjects)
	began = time.Now()
	var sending sync.WaitGroup
	for i, manager := range managers {
		sending.Go(func() {
			for sequence := range frames {
				size := 64
				if sequence%2 != 0 {
					size = 64 << 10
				}
				data := bytes.Repeat([]byte{byte(sequence)}, size)
				binary.BigEndian.PutUint64(data, uint64(sequence))
				binary.BigEndian.PutUint64(data[8:], uint64(time.Now().UnixNano()))
				if err := manager.(ContextConnectionManager).SendToNodeContext(ctx, ids[(i+1)%nodes], data, ClassPGBroadcast); err != nil {
					report(err)
					return
				}
			}
		})
	}
	sending.Wait()
	for _, done := range completions {
		select {
		case <-done:
		case err := <-failures:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	select {
	case err := <-failures:
		t.Fatal(err)
	default:
	}
	samples := make([]time.Duration, 0, nodes*frames)
	for i := range nodes {
		require.Equal(t, int32(frames), counts[i].Load())
		samples = append(samples, latencies[i]...)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	t.Logf("mixed ring load over full mesh: %d messages, %d bytes in %s; admission-to-callback-entry p50=%s p95=%s p99=%s", len(samples), nodes*(frames/2)*(64+(64<<10)), time.Since(began), samples[len(samples)/2], samples[len(samples)*95/100], samples[len(samples)*99/100])
}
