// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

func benchmarkThroughput(b *testing.B, size int) {
	start := func(self, peer cluster.NodeID, recv func()) *manager {
		cfg := insecureManagerConfig()
		cfg.LocalNodeID = self
		cfg.BindAddr = "127.0.0.1"
		cfg.BindPort = 0
		cfg.Logger = zap.NewNop()
		m := NewConnectionManager(cfg, nil).(*manager)
		if err := m.Start(context.Background(), func(cluster.NodeID, []byte) { recv() }, ignoreSessionEnd); err != nil {
			b.Fatal(err)
		}
		m.AddManagedNode(peer)
		return m
	}
	var got atomic.Int64
	a := start("node-a", "node-b", func() {})
	defer func() { _ = a.Stop() }()
	bm := start("node-b", "node-a", func() { got.Add(1) })
	defer func() { _ = bm.Stop() }()
	a.EnsureConnection("node-b", "127.0.0.1", bm.GetListenPort())
	for {
		if _, s := a.nodeStates.GetNodeConnection("node-b"); s == StateConnected {
			break
		}
		time.Sleep(time.Millisecond)
	}
	payload := make([]byte, size)
	b.SetBytes(int64(size))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := a.SendToNode("node-b", payload, ClassPGBroadcast); err != nil {
			b.Fatal(err)
		}
	}
	for got.Load() < int64(b.N) {
		time.Sleep(50 * time.Microsecond)
	}
}

func BenchmarkThroughput256(b *testing.B) { benchmarkThroughput(b, 256) }
func BenchmarkThroughput16K(b *testing.B) { benchmarkThroughput(b, 16<<10) }
