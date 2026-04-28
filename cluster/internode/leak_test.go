// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"runtime"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

// TestQueueGrowsWithoutBound proves the current bug: a stuck peer's
// messageQueue has no upper limit. After this fix, the test will be
// rewritten to assert the bound is enforced.
func TestQueueGrowsWithoutBound(t *testing.T) {
	t.Skip("baseline reproducer - delete in Task 1 once bounds are enforced")

	logger := zap.NewNop()
	cfg := DefaultManagerConfig()
	cfg.Logger = logger
	nsm := NewNodeStateManager(cfg, logger)
	const node cluster.NodeID = "stuck-peer"
	nsm.CreateNodeState(node)

	var beforeMS, afterMS runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&beforeMS)

	for i := 0; i < 200_000; i++ {
		_ = nsm.QueueMessage(node, []byte("payload-payload-payload-payload"))
	}

	runtime.GC()
	runtime.ReadMemStats(&afterMS)
	growth := afterMS.HeapAlloc - beforeMS.HeapAlloc

	t.Logf("heap growth after 200k queued messages on stuck peer: %d bytes", growth)
	// No assertion: the test exists to demonstrate that growth is unbounded.
	// Task 1 will replace this file with a bounded-queue assertion.
	time.Sleep(10 * time.Millisecond)
}
