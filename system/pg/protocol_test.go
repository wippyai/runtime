// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
)

func TestServicePID(t *testing.T) {
	svc, _, _ := newTestService()
	p := svc.servicePID("node-a")
	assert.Equal(t, pid.NodeID("node-a"), p.Node)
	assert.Equal(t, pid.HostID("pg"), p.Host)
	assert.Equal(t, "", p.UniqID)
}

func TestSendDiscover(t *testing.T) {
	svc, router, _ := startTestService(t)

	router.reset()

	svc.submit(func() {
		svc.sendDiscover("node-b")
	})

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	require.Len(t, sends, 1)

	pkg := sends[0]
	require.NotEmpty(t, pkg.Messages)
	assert.Equal(t, "pg.discover", pkg.Messages[0].Topic)
}

func TestSendSync(t *testing.T) {
	svc, router, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	router.reset()

	svc.submit(func() {
		svc.sendSync("node-b")
	})

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	require.Len(t, sends, 1)

	pkg := sends[0]
	require.NotEmpty(t, pkg.Messages)
	assert.Equal(t, "pg.sync", pkg.Messages[0].Topic)
}

func TestBroadcastJoinToRemoteNodes(t *testing.T) {
	svc, router, _ := startTestService(t)

	// Register a remote node so broadcastJoin has targets
	svc.submit(func() {
		svc.state.remote["node-b"] = &remoteNode{
			nodeID: "node-b",
			groups: make(map[string][]pid.PID),
		}
	})
	time.Sleep(20 * time.Millisecond)

	router.reset()

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	// Should have sent a join broadcast to node-b
	found := false
	for _, pkg := range sends {
		for _, msg := range pkg.Messages {
			if msg.Topic == "pg.join" {
				found = true
			}
		}
	}
	assert.True(t, found, "expected a pg.join message to be broadcast")
}

func TestBroadcastLeaveToRemoteNodes(t *testing.T) {
	svc, router, _ := startTestService(t)

	// Register a remote node
	svc.submit(func() {
		svc.state.remote["node-b"] = &remoteNode{
			nodeID: "node-b",
			groups: make(map[string][]pid.PID),
		}
	})
	time.Sleep(20 * time.Millisecond)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	router.reset()

	require.NoError(t, svc.Leave("workers", p1))

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	found := false
	for _, pkg := range sends {
		for _, msg := range pkg.Messages {
			if msg.Topic == "pg.leave" {
				found = true
			}
		}
	}
	assert.True(t, found, "expected a pg.leave message to be broadcast")
}

func TestBroadcastLeaveEmptyPids(t *testing.T) {
	svc, router, _ := startTestService(t)

	svc.submit(func() {
		svc.state.remote["node-b"] = &remoteNode{
			nodeID: "node-b",
			groups: make(map[string][]pid.PID),
		}
	})
	time.Sleep(20 * time.Millisecond)

	router.reset()

	svc.submit(func() {
		svc.broadcastLeave(map[string][]pid.PID{"workers": nil})
	})

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	assert.Empty(t, sends, "broadcastLeave with empty pids should not send anything")
}

func TestHandleDiscover(t *testing.T) {
	svc, router, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	router.reset()

	// Simulate a discover from node-b
	svc.submit(func() {
		svc.handleDiscover("node-b")
	})

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	// Should have sent a sync AND a discover back
	hasSync := false
	hasDiscover := false
	for _, pkg := range sends {
		for _, msg := range pkg.Messages {
			if msg.Topic == "pg.sync" {
				hasSync = true
			}
			if msg.Topic == "pg.discover" {
				hasDiscover = true
			}
		}
	}
	assert.True(t, hasSync, "expected a pg.sync response")
	assert.True(t, hasDiscover, "expected a pg.discover back to new node")
}

func TestHandleDiscoverExistingNode(t *testing.T) {
	svc, router, _ := startTestService(t)

	// Pre-register node-b
	svc.submit(func() {
		svc.state.remote["node-b"] = &remoteNode{
			nodeID: "node-b",
			groups: make(map[string][]pid.PID),
		}
	})
	time.Sleep(20 * time.Millisecond)

	router.reset()

	// Discover from already-known node-b
	svc.submit(func() {
		svc.handleDiscover("node-b")
	})

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	// Should send sync but NOT discover back (already known)
	hasSync := false
	hasDiscover := false
	for _, pkg := range sends {
		for _, msg := range pkg.Messages {
			if msg.Topic == "pg.sync" {
				hasSync = true
			}
			if msg.Topic == "pg.discover" {
				hasDiscover = true
			}
		}
	}
	assert.True(t, hasSync, "expected a pg.sync response")
	assert.False(t, hasDiscover, "should not discover back to already-known node")
}

func TestHandleSync(t *testing.T) {
	svc, _, _ := startTestService(t)

	rp1 := mkNodePID("node-b", "host1", "1")
	rp2 := mkNodePID("node-b", "host1", "2")

	svc.submit(func() {
		svc.handleSync("node-b", map[string][]pid.PID{
			"workers": {rp1, rp2},
		})
		svc.publishDirty()
	})

	time.Sleep(50 * time.Millisecond)

	members := svc.GetMembers("workers")
	assert.Len(t, members, 2)
}

func TestHandleRemoteJoin(t *testing.T) {
	svc, _, _ := startTestService(t)

	rp1 := mkNodePID("node-b", "host1", "1")

	svc.submit(func() {
		svc.handleRemoteJoin("node-b", "workers", []pid.PID{rp1})
		svc.publishDirty()
	})

	time.Sleep(50 * time.Millisecond)

	members := svc.GetMembers("workers")
	require.Len(t, members, 1)
	assert.Equal(t, rp1.String(), members[0].String())

	// Not a local member
	localMembers := svc.GetLocalMembers("workers")
	assert.Empty(t, localMembers)
}

func TestHandleRemoteLeave(t *testing.T) {
	svc, _, _ := startTestService(t)

	rp1 := mkNodePID("node-b", "host1", "1")

	svc.submit(func() {
		svc.handleRemoteJoin("node-b", "workers", []pid.PID{rp1})
		svc.publishDirty()
	})
	time.Sleep(20 * time.Millisecond)

	svc.submit(func() {
		svc.handleRemoteLeave("node-b", []pid.PID{rp1}, []string{"workers"})
		svc.publishDirty()
	})
	time.Sleep(50 * time.Millisecond)

	members := svc.GetMembers("workers")
	assert.Empty(t, members)
}

func TestHandleRemoteLeaveMultiGroupOnlyEmitsForActualGroups(t *testing.T) {
	svc, _, _ := startTestService(t)

	rp1 := mkNodePID("node-b", "host1", "1")

	// rp1 joins "workers" but NOT "managers"
	svc.submit(func() {
		svc.handleRemoteJoin("node-b", "workers", []pid.PID{rp1})
		svc.publishDirty()
	})
	time.Sleep(20 * time.Millisecond)

	assert.Len(t, svc.GetMembers("workers"), 1)
	assert.Empty(t, svc.GetMembers("managers"))

	// Leave both groups — only "workers" should be affected
	svc.submit(func() {
		svc.handleRemoteLeave("node-b", []pid.PID{rp1}, []string{"workers", "managers"})
		svc.publishDirty()
	})
	time.Sleep(50 * time.Millisecond)

	assert.Empty(t, svc.GetMembers("workers"))
	assert.Empty(t, svc.GetMembers("managers"))
}

func TestHandleRemoteLeaveDoesNotCorruptOtherNodeState(t *testing.T) {
	svc, _, _ := startTestService(t)

	rp1 := mkNodePID("node-b", "host1", "1")
	rp2 := mkNodePID("node-c", "host1", "2")

	// rp1 (node-b) in "workers", rp2 (node-c) in "workers" and "managers"
	svc.submit(func() {
		svc.handleRemoteJoin("node-b", "workers", []pid.PID{rp1})
		svc.handleRemoteJoin("node-c", "workers", []pid.PID{rp2})
		svc.handleRemoteJoin("node-c", "managers", []pid.PID{rp2})
		svc.publishDirty()
	})
	time.Sleep(20 * time.Millisecond)

	assert.Len(t, svc.GetMembers("workers"), 2)
	assert.Len(t, svc.GetMembers("managers"), 1)

	// Leave rp1 from both "workers" and "managers" on node-b.
	// rp1 was never in "managers" on node-b, so rp2's membership must be preserved.
	svc.submit(func() {
		svc.handleRemoteLeave("node-b", []pid.PID{rp1}, []string{"workers", "managers"})
		svc.publishDirty()
	})
	time.Sleep(50 * time.Millisecond)

	// rp2 should remain in both groups
	workers := svc.GetMembers("workers")
	require.Len(t, workers, 1)
	assert.Equal(t, rp2.String(), workers[0].String())

	managers := svc.GetMembers("managers")
	require.Len(t, managers, 1)
	assert.Equal(t, rp2.String(), managers[0].String())
}

func TestHandleProcessExit(t *testing.T) {
	svc, _, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("managers", p1))

	svc.submit(func() {
		svc.handleProcessExit(p1)
		svc.publishDirty()
	})

	time.Sleep(50 * time.Millisecond)

	assert.Empty(t, svc.GetMembers("workers"))
	assert.Empty(t, svc.GetMembers("managers"))
}

func TestHandleProcessExitNotJoined(t *testing.T) {
	svc, _, _ := startTestService(t)

	p1 := mkPID("host1", "1")

	// Should not panic
	svc.submit(func() {
		svc.handleProcessExit(p1)
	})

	time.Sleep(50 * time.Millisecond)
}

func TestHandleNodeLeft(t *testing.T) {
	svc, _, _ := startTestService(t)

	rp1 := mkNodePID("node-b", "host1", "1")
	rp2 := mkNodePID("node-b", "host1", "2")

	svc.submit(func() {
		svc.handleSync("node-b", map[string][]pid.PID{
			"workers": {rp1, rp2},
		})
		svc.publishDirty()
	})
	time.Sleep(20 * time.Millisecond)

	assert.Len(t, svc.GetMembers("workers"), 2)

	svc.submit(func() {
		svc.handleNodeLeft("node-b")
		svc.publishDirty()
	})

	time.Sleep(50 * time.Millisecond)

	assert.Empty(t, svc.GetMembers("workers"))
}

func TestHandleNodeLeftUnknown(t *testing.T) {
	svc, _, _ := startTestService(t)

	// Should not panic
	svc.submit(func() {
		svc.handleNodeLeft("unknown-node")
	})

	time.Sleep(50 * time.Millisecond)
}

func TestMonitorProcess(t *testing.T) {
	svc, _, topo := startTestService(t)

	p1 := mkPID("host1", "1")

	svc.submit(func() {
		svc.monitorProcess(p1)
	})

	time.Sleep(50 * time.Millisecond)

	assert.True(t, topo.isMonitored(p1))
}

func TestDemonitorProcess(t *testing.T) {
	svc, _, topo := startTestService(t)

	p1 := mkPID("host1", "1")

	svc.submit(func() {
		svc.monitorProcess(p1)
	})
	time.Sleep(20 * time.Millisecond)
	assert.True(t, topo.isMonitored(p1))

	svc.submit(func() {
		svc.demonitorProcess(p1)
	})
	time.Sleep(50 * time.Millisecond)

	assert.False(t, topo.isMonitored(p1))
}

func TestProcessExitBroadcastsLeave(t *testing.T) {
	svc, router, _ := startTestService(t)

	// Register a remote node for leave broadcasts
	svc.submit(func() {
		svc.state.remote["node-b"] = &remoteNode{
			nodeID: "node-b",
			groups: make(map[string][]pid.PID),
		}
	})
	time.Sleep(20 * time.Millisecond)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	router.reset()

	svc.submit(func() {
		svc.handleProcessExit(p1)
	})

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	found := false
	for _, pkg := range sends {
		for _, msg := range pkg.Messages {
			if msg.Topic == "pg.leave" {
				found = true
			}
		}
	}
	assert.True(t, found, "expected a pg.leave broadcast on process exit")
}

// --- monitorProcess error path tests ---

func TestMonitorProcessAlreadyMonitoring(t *testing.T) {
	svc, _, topo := startTestService(t)

	p1 := mkPID("host1", "1")

	// Set monitorErr to ErrAlreadyMonitoring — should be silently ignored
	topo.mu.Lock()
	topo.monitorErr = topology.ErrAlreadyMonitoring
	topo.mu.Unlock()

	svc.submit(func() {
		svc.monitorProcess(p1)
	})

	time.Sleep(50 * time.Millisecond)

	// Should not panic or log warning — just ignored
}

func TestMonitorProcessUnexpectedError(t *testing.T) {
	svc, _, topo := startTestService(t)

	p1 := mkPID("host1", "1")

	// Set a non-ErrAlreadyMonitoring error — triggers the warn branch
	topo.mu.Lock()
	topo.monitorErr = errors.New("unexpected topology error")
	topo.mu.Unlock()

	svc.submit(func() {
		svc.monitorProcess(p1)
	})

	time.Sleep(50 * time.Millisecond)

	// Should not panic — the warn log was exercised
}

// --- sendDiscover / sendSync router error path tests ---

func TestSendDiscoverRouterError(t *testing.T) {
	svc, router, _ := startTestService(t)

	// Make router fail
	router.mu.Lock()
	router.sendErr = errors.New("router send failed")
	router.mu.Unlock()

	router.reset()

	svc.submit(func() {
		svc.sendDiscover("node-b")
	})

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	assert.Empty(t, sends, "router rejected the discover send")
	// The warn log branch was exercised
}

func TestSendSyncRouterError(t *testing.T) {
	svc, router, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	// Make router fail
	router.mu.Lock()
	router.sendErr = errors.New("router send failed")
	router.mu.Unlock()

	router.reset()

	svc.submit(func() {
		svc.sendSync("node-b")
	})

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	assert.Empty(t, sends, "router rejected the sync send")
}

// --- broadcastJoin / broadcastLeave router error path tests ---

func TestBroadcastJoinRouterError(t *testing.T) {
	svc, router, _ := startTestService(t)

	// Register a remote node
	svc.submit(func() {
		svc.state.remote["node-b"] = &remoteNode{
			nodeID: "node-b",
			groups: make(map[string][]pid.PID),
		}
	})
	time.Sleep(20 * time.Millisecond)

	// Make router fail
	router.mu.Lock()
	router.sendErr = errors.New("router send failed")
	router.mu.Unlock()

	router.reset()

	p1 := mkPID("host1", "1")
	svc.submit(func() {
		svc.broadcastJoin(map[string][]pid.PID{"workers": {p1}})
	})

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	assert.Empty(t, sends, "router rejected the join broadcast")
}

func TestBroadcastLeaveRouterError(t *testing.T) {
	svc, router, _ := startTestService(t)

	// Register a remote node
	svc.submit(func() {
		svc.state.remote["node-b"] = &remoteNode{
			nodeID: "node-b",
			groups: make(map[string][]pid.PID),
		}
	})
	time.Sleep(20 * time.Millisecond)

	// Make router fail
	router.mu.Lock()
	router.sendErr = errors.New("router send failed")
	router.mu.Unlock()

	router.reset()

	p1 := mkPID("host1", "1")
	svc.submit(func() {
		svc.broadcastLeave(map[string][]pid.PID{"workers": {p1}})
	})

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	assert.Empty(t, sends, "router rejected the leave broadcast")
}

// --- broadcastJoin / broadcastLeave to multiple remote nodes ---

func TestBroadcastJoinMultipleRemoteNodes(t *testing.T) {
	svc, router, _ := startTestService(t)

	// Register two remote nodes
	svc.submit(func() {
		svc.state.remote["node-b"] = &remoteNode{
			nodeID: "node-b",
			groups: make(map[string][]pid.PID),
		}
		svc.state.remote["node-c"] = &remoteNode{
			nodeID: "node-c",
			groups: make(map[string][]pid.PID),
		}
	})
	time.Sleep(20 * time.Millisecond)

	router.reset()

	p1 := mkPID("host1", "1")
	svc.submit(func() {
		svc.broadcastJoin(map[string][]pid.PID{"workers": {p1}})
	})

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	joinCount := 0
	for _, pkg := range sends {
		for _, msg := range pkg.Messages {
			if msg.Topic == "pg.join" {
				joinCount++
			}
		}
	}
	assert.Equal(t, 2, joinCount, "should broadcast join to both remote nodes")
}

func TestBroadcastLeaveMultipleRemoteNodes(t *testing.T) {
	svc, router, _ := startTestService(t)

	// Register two remote nodes
	svc.submit(func() {
		svc.state.remote["node-b"] = &remoteNode{
			nodeID: "node-b",
			groups: make(map[string][]pid.PID),
		}
		svc.state.remote["node-c"] = &remoteNode{
			nodeID: "node-c",
			groups: make(map[string][]pid.PID),
		}
	})
	time.Sleep(20 * time.Millisecond)

	router.reset()

	p1 := mkPID("host1", "1")
	svc.submit(func() {
		svc.broadcastLeave(map[string][]pid.PID{"workers": {p1}})
	})

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	leaveCount := 0
	for _, pkg := range sends {
		for _, msg := range pkg.Messages {
			if msg.Topic == "pg.leave" {
				leaveCount++
			}
		}
	}
	assert.Equal(t, 2, leaveCount, "should broadcast leave to both remote nodes")
}

func TestBroadcastLeaveEmptyGroups(t *testing.T) {
	svc, router, _ := startTestService(t)

	svc.submit(func() {
		svc.state.remote["node-b"] = &remoteNode{
			nodeID: "node-b",
			groups: make(map[string][]pid.PID),
		}
	})
	time.Sleep(20 * time.Millisecond)

	router.reset()

	svc.submit(func() {
		svc.broadcastLeave(map[string][]pid.PID{}) // empty groups map
	})

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	assert.Empty(t, sends, "broadcastLeave with empty groups should not send anything")
}

// --- handleProcessExit with remote broadcast ---

func TestHandleProcessExitMultipleGroups(t *testing.T) {
	svc, router, _ := startTestService(t)

	// Register remote node
	svc.submit(func() {
		svc.state.remote["node-b"] = &remoteNode{
			nodeID: "node-b",
			groups: make(map[string][]pid.PID),
		}
	})
	time.Sleep(20 * time.Millisecond)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("managers", p1))
	require.NoError(t, svc.Join("workers", p1)) // multi-join

	router.reset()

	svc.submit(func() {
		svc.handleProcessExit(p1)
		svc.publishDirty()
	})

	time.Sleep(50 * time.Millisecond)

	// Should have cleared all memberships
	assert.Empty(t, svc.GetMembers("workers"))
	assert.Empty(t, svc.GetMembers("managers"))

	// And broadcast leave to remote nodes
	sends := router.getSends()
	found := false
	for _, pkg := range sends {
		for _, msg := range pkg.Messages {
			if msg.Topic == "pg.leave" {
				found = true
			}
		}
	}
	assert.True(t, found, "expected leave broadcast on multi-group exit")
}
