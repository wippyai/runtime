// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/internal/telemetrytest"
	"go.uber.org/zap"
)

// mockRouter records all sent packages.
type mockRouter struct {
	sendErr error
	sends   []*relay.Package
	mu      sync.Mutex
}

func newMockRouter() *mockRouter {
	return &mockRouter{}
}

func (m *mockRouter) Send(pkg *relay.Package) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sends = append(m.sends, pkg)
	return nil
}

func (m *mockRouter) getSends() []*relay.Package {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*relay.Package, len(m.sends))
	copy(result, m.sends)
	return result
}

func (m *mockRouter) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends = nil
}

// mockTopology records monitor/demonitor calls.
type mockTopology struct {
	monitored  map[string]bool // target pid string -> true
	monitorErr error
	mu         sync.Mutex
}

func newMockTopology() *mockTopology {
	return &mockTopology{
		monitored: make(map[string]bool),
	}
}

func (m *mockTopology) Register(pid.PID) error            { return nil }
func (m *mockTopology) Complete(pid.PID, *runtime.Result) {}
func (m *mockTopology) Remove(pid.PID)                    {}
func (m *mockTopology) Link(_, _ pid.PID) error           { return nil }
func (m *mockTopology) Unlink(_, _ pid.PID) error         { return nil }
func (m *mockTopology) GetLinks(pid.PID) []pid.PID        { return nil }

func (m *mockTopology) Monitor(caller, target pid.PID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.monitorErr != nil {
		return m.monitorErr
	}
	m.monitored[target.String()] = true
	return nil
}

func (m *mockTopology) Demonitor(caller, target pid.PID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.monitored, target.String())
	return nil
}

func (m *mockTopology) isMonitored(p pid.PID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.monitored[p.String()]
}

// testServicePID is a test helper that builds a service PID for the
// default "pg" host ID. Service PIDs have an empty UniqID since PG
// is a host-level receiver and UniqID is not used for routing.
func testServicePID(nodeID pid.NodeID) pid.PID {
	return pid.PID{
		Node: nodeID,
		Host: "pg",
	}
}

func newTestService() (*Service, *mockRouter, *mockTopology) {
	router := newMockRouter()
	topo := newMockTopology()
	logger := zap.NewNop()
	svc := NewService(logger, "pg", nil, router, topo, nil, nil, "local-node", nil, nil, nil)
	return svc, router, topo
}

func startTestService(t *testing.T) (*Service, *mockRouter, *mockTopology) {
	t.Helper()
	svc, router, topo := newTestService()
	_, err := svc.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = svc.Stop(context.Background())
	})
	// Small delay to let event loop start
	time.Sleep(10 * time.Millisecond)
	return svc, router, topo
}

func TestNewService(t *testing.T) {
	svc, _, _ := newTestService()
	require.NotNil(t, svc)
	assert.Equal(t, pid.NodeID("local-node"), svc.localNodeID)
}

// TestRemoveMonitorsByNode verifies the eviction path that runs when a
// remote node leaves the cluster. Without it the monitors map would
// leak entries for every PID hosted on the departed node, eventually
// causing OOM under partition / pod-kill chaos. Regression guard for
// the unbounded growth audited in REGRESSIONS.md row #3.
func TestRemoveMonitorsByNode(t *testing.T) {
	svc, _, _ := newTestService()

	// Monitors live on the service struct directly; calling the eviction
	// helper requires no Start/event-loop setup.
	pidA := mkNodePID("alpha", "h", "1")
	pidA2 := mkNodePID("alpha", "h", "2")
	pidB := mkNodePID("beta", "h", "1")

	add := func(group string, p pid.PID) {
		svc.monitors[group] = append(svc.monitors[group], &monitorEntry{
			pid:   p,
			topic: "test",
			id:    1,
		})
		svc.monitorPIDCounts[p.String()]++
	}
	add("g1", pidA)
	add("g1", pidA2)
	add("g1", pidB)
	add("g2", pidA)

	evicted := svc.removeMonitorsByNode("alpha")
	require.Equal(t, 3, evicted)

	// Group g1 must keep only pidB; g2 must be deleted entirely.
	require.Len(t, svc.monitors["g1"], 1)
	require.Equal(t, pidB.String(), svc.monitors["g1"][0].pid.String())
	_, exists := svc.monitors["g2"]
	require.False(t, exists, "group with no remaining monitors must be deleted")

	// PID counts for evicted PIDs must be zero (deleted from the map).
	_, hasA := svc.monitorPIDCounts[pidA.String()]
	_, hasA2 := svc.monitorPIDCounts[pidA2.String()]
	require.False(t, hasA)
	require.False(t, hasA2)
	require.Equal(t, 1, svc.monitorPIDCounts[pidB.String()])
}

func TestRemoveMonitorsByNode_Empty(t *testing.T) {
	svc, _, _ := newTestService()
	require.Equal(t, 0, svc.removeMonitorsByNode("ghost"))
	require.Equal(t, 0, svc.removeMonitorsByNode(""))
}

func TestNewServiceNilLogger(t *testing.T) {
	svc := NewService(nil, "pg", nil, newMockRouter(), newMockTopology(), nil, nil, "node", nil, nil, nil)
	require.NotNil(t, svc)
}

func TestServiceStartStop(t *testing.T) {
	svc, _, _ := newTestService()

	_, err := svc.Start(context.Background())
	require.NoError(t, err)

	err = svc.Stop(context.Background())
	require.NoError(t, err)
}

func TestServiceJoin(t *testing.T) {
	svc, _, topo := startTestService(t)

	p1 := mkPID("host1", "1")
	err := svc.Join("workers", p1)
	require.NoError(t, err)

	members := svc.GetMembers("workers")
	require.Len(t, members, 1)
	assert.Equal(t, p1.String(), members[0].String())

	// Should be monitored
	assert.True(t, topo.isMonitored(p1))
}

func TestServiceJoinMultiple(t *testing.T) {
	svc, _, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")

	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("workers", p2))

	members := svc.GetMembers("workers")
	assert.Len(t, members, 2)
}

func TestServiceLeave(t *testing.T) {
	svc, _, topo := startTestService(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	err := svc.Leave("workers", p1)
	require.NoError(t, err)

	members := svc.GetMembers("workers")
	assert.Empty(t, members)

	// Should be demonitored
	assert.False(t, topo.isMonitored(p1))
}

func TestServiceLeaveNotJoined(t *testing.T) {
	svc, _, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	err := svc.Leave("workers", p1)
	assert.ErrorIs(t, err, ErrNotJoined)
}

func TestServiceGetMembers(t *testing.T) {
	svc, _, _ := startTestService(t)

	// Empty group
	members := svc.GetMembers("nonexistent")
	assert.Nil(t, members)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	members = svc.GetMembers("workers")
	assert.Len(t, members, 1)
}

func TestServiceGetLocalMembers(t *testing.T) {
	svc, _, _ := startTestService(t)

	members := svc.GetLocalMembers("nonexistent")
	assert.Nil(t, members)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	members = svc.GetLocalMembers("workers")
	assert.Len(t, members, 1)
}

func TestServiceWhichGroups(t *testing.T) {
	svc, _, _ := startTestService(t)

	groups := svc.WhichGroups()
	assert.Empty(t, groups)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("managers", p1))

	groups = svc.WhichGroups()
	assert.Len(t, groups, 2)
}

func TestServiceBroadcast(t *testing.T) {
	svc, router, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")
	sender := mkPID("host1", "sender")

	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("workers", p2))

	router.reset() // Clear any join-related sends

	sent, err := svc.Broadcast(sender, "workers", "hello", nil)
	require.NoError(t, err)
	assert.Equal(t, 2, sent)

	// Allow time for async processing
	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	assert.Len(t, sends, 2)
}

func TestServiceBroadcastLocal(t *testing.T) {
	svc, router, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	sender := mkPID("host1", "sender")

	require.NoError(t, svc.Join("workers", p1))

	router.reset()

	sent, err := svc.BroadcastLocal(sender, "workers", "hello", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, sent)

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	assert.Len(t, sends, 1)
}

func TestServiceBroadcastEmptyGroup(t *testing.T) {
	svc, router, _ := startTestService(t)

	sender := mkPID("host1", "sender")

	router.reset()

	sent, err := svc.Broadcast(sender, "empty", "hello", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, sent)

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	assert.Empty(t, sends)
}

func TestServiceRelayReceiver(t *testing.T) {
	svc, _, _ := newTestService()

	// Test nil package
	err := svc.Send(nil)
	assert.NoError(t, err)

	// Test empty package
	err = svc.Send(&relay.Package{})
	assert.NoError(t, err)
}

func TestServiceInterfaceCompliance(t *testing.T) {
	svc, _, _ := newTestService()
	var _ relay.Receiver = svc
}

// --- mockMembership for testing Start() with cluster discovery ---

type mockMembership struct {
	localNode cluster.NodeInfo
	nodes     []cluster.NodeInfo
}

func (m *mockMembership) Nodes() []cluster.NodeInfo   { return m.nodes }
func (m *mockMembership) LocalNode() cluster.NodeInfo { return m.localNode }

// --- Send() relay package parsing tests ---

func TestServiceSend_DiscoverPackage(t *testing.T) {
	svc, router, _ := startTestService(t)

	router.reset()

	// Build a discover relay package
	source := testServicePID("node-b")
	target := testServicePID("local-node")
	pkg := relay.NewPackage(source, target, pgapi.TopicDiscover,
		payload.New(map[string]any{
			"from": "node-b",
		}),
	)

	err := svc.Send(pkg)
	require.NoError(t, err)

	// Allow action to be processed
	time.Sleep(50 * time.Millisecond)

	// Should have sent sync back to node-b, and discover back
	sends := router.getSends()
	require.NotEmpty(t, sends)

	hasSync := false
	for _, s := range sends {
		for _, msg := range s.Messages {
			if msg.Topic == pgapi.TopicSync {
				hasSync = true
			}
		}
	}
	assert.True(t, hasSync, "expected sync response to discover")
}

func TestServiceSend_SyncPackage(t *testing.T) {
	svc, _, _ := startTestService(t)

	// Build a sync relay package with groups data
	rp1 := mkNodePID("node-b", "host1", "1")
	source := testServicePID("node-b")
	target := testServicePID("local-node")
	pkg := relay.NewPackage(source, target, pgapi.TopicSync,
		payload.New(map[string]any{
			"from": "node-b",
			"groups": map[string]any{
				"workers": []any{rp1.String()},
			},
		}),
	)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	members := svc.GetMembers("workers")
	assert.Len(t, members, 1)
}

func TestServiceSend_JoinPackage(t *testing.T) {
	svc, _, _ := startTestService(t)

	rp1 := mkNodePID("node-b", "host1", "1")
	source := testServicePID("node-b")
	target := testServicePID("local-node")
	pkg := relay.NewPackage(source, target, pgapi.TopicJoin,
		payload.New(map[string]any{
			"from":  "node-b",
			"group": "workers",
			"pids":  []any{rp1.String()},
		}),
	)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	members := svc.GetMembers("workers")
	require.Len(t, members, 1)
	assert.Equal(t, rp1.String(), members[0].String())
}

func TestServiceSend_LeavePackage(t *testing.T) {
	svc, _, _ := startTestService(t)

	// First join remotely
	rp1 := mkNodePID("node-b", "host1", "1")
	joinPkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicJoin,
		payload.New(map[string]any{
			"from":  "node-b",
			"group": "workers",
			"pids":  []any{rp1.String()},
		}),
	)
	require.NoError(t, svc.Send(joinPkg))
	time.Sleep(50 * time.Millisecond)

	require.Len(t, svc.GetMembers("workers"), 1)

	// Now leave
	leavePkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicLeave,
		payload.New(map[string]any{
			"from":   "node-b",
			"pids":   []any{rp1.String()},
			"groups": []any{"workers"},
		}),
	)
	require.NoError(t, svc.Send(leavePkg))
	time.Sleep(50 * time.Millisecond)

	assert.Empty(t, svc.GetMembers("workers"))
}

func TestServiceSend_ExitPackage(t *testing.T) {
	svc, _, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))
	require.Len(t, svc.GetMembers("workers"), 1)

	// Send exit event via relay
	exitEvent := &topology.ExitEvent{From: p1}
	msg := relay.AcquireMessage()
	msg.Topic = topology.TopicEvents
	msg.Payloads = payload.Payloads{payload.New(exitEvent)}
	pkg := relay.NewMessagePackage(p1, testServicePID("local-node"), msg)

	require.NoError(t, svc.Send(pkg))
	time.Sleep(50 * time.Millisecond)

	assert.Empty(t, svc.GetMembers("workers"))
}

// --- Send() malformed payload tests ---

func TestServiceSend_DiscoverNoPayloads(t *testing.T) {
	svc, router, _ := startTestService(t)

	router.reset()

	msg := relay.AcquireMessage()
	msg.Topic = pgapi.TopicDiscover
	// No payloads
	pkg := relay.NewMessagePackage(testServicePID("node-b"), testServicePID("local-node"), msg)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Should not have sent anything since discover had no payload
	sends := router.getSends()
	assert.Empty(t, sends)
}

func TestServiceSend_DiscoverWrongPayloadType(t *testing.T) {
	svc, router, _ := startTestService(t)

	router.reset()

	pkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicDiscover,
		payload.New("not a map"),
	)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	assert.Empty(t, sends)
}

func TestServiceSend_DiscoverEmptyFromField(t *testing.T) {
	svc, router, _ := startTestService(t)

	router.reset()

	pkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicDiscover,
		payload.New(map[string]any{
			"from": "", // empty from
		}),
	)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	assert.Empty(t, sends)
}

func TestServiceSend_SyncNoPayloads(t *testing.T) {
	svc, _, _ := startTestService(t)

	msg := relay.AcquireMessage()
	msg.Topic = pgapi.TopicSync
	pkg := relay.NewMessagePackage(testServicePID("node-b"), testServicePID("local-node"), msg)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	// Should not crash
}

func TestServiceSend_SyncWrongPayloadType(t *testing.T) {
	svc, _, _ := startTestService(t)

	pkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicSync,
		payload.New(42), // not a map
	)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	// Should not crash
}

func TestServiceSend_SyncEmptyFrom(t *testing.T) {
	svc, _, _ := startTestService(t)

	pkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicSync,
		payload.New(map[string]any{
			"from":   "",
			"groups": map[string]any{},
		}),
	)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	// Should not crash, should be ignored
}

func TestServiceSend_JoinNoPayloads(t *testing.T) {
	svc, _, _ := startTestService(t)

	msg := relay.AcquireMessage()
	msg.Topic = pgapi.TopicJoin
	pkg := relay.NewMessagePackage(testServicePID("node-b"), testServicePID("local-node"), msg)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
}

func TestServiceSend_JoinMissingGroup(t *testing.T) {
	svc, _, _ := startTestService(t)

	pkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicJoin,
		payload.New(map[string]any{
			"from": "node-b",
			// missing "group"
			"pids": []any{"{host1|1}"},
		}),
	)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, svc.GetMembers(""))
}

func TestServiceSend_JoinMissingFrom(t *testing.T) {
	svc, _, _ := startTestService(t)

	pkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicJoin,
		payload.New(map[string]any{
			"group": "workers",
			"pids":  []any{"{host1|1}"},
		}),
	)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	// Should be ignored — no join happened
	assert.Empty(t, svc.GetMembers("workers"))
}

func TestServiceSend_JoinInvalidPIDStrings(t *testing.T) {
	svc, _, _ := startTestService(t)

	pkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicJoin,
		payload.New(map[string]any{
			"from":  "node-b",
			"group": "workers",
			"pids":  []any{"not-a-valid-pid", 123, "{host1|1}"}, // mixed invalid + valid
		}),
	)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	// Only the valid PID should have been joined
	members := svc.GetMembers("workers")
	assert.Len(t, members, 1)
}

func TestServiceSend_LeaveNoPayloads(t *testing.T) {
	svc, _, _ := startTestService(t)

	msg := relay.AcquireMessage()
	msg.Topic = pgapi.TopicLeave
	pkg := relay.NewMessagePackage(testServicePID("node-b"), testServicePID("local-node"), msg)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
}

func TestServiceSend_LeaveMissingFrom(t *testing.T) {
	svc, _, _ := startTestService(t)

	pkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicLeave,
		payload.New(map[string]any{
			// missing "from"
			"pids":   []any{"{host1|1}"},
			"groups": []any{"workers"},
		}),
	)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
}

func TestServiceSend_SyncWithInvalidPIDStrings(t *testing.T) {
	svc, _, _ := startTestService(t)

	pkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicSync,
		payload.New(map[string]any{
			"from": "node-b",
			"groups": map[string]any{
				"workers": []any{"invalid-pid", "{host1|1}"},
				"empty":   []any{"also-invalid"},
			},
		}),
	)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Only valid PID parsed
	members := svc.GetMembers("workers")
	assert.Len(t, members, 1)

	// "empty" group should not appear — all PIDs invalid
	emptyMembers := svc.GetMembers("empty")
	assert.Nil(t, emptyMembers)
}

func TestServiceSend_SyncWithNonSliceGroupValue(t *testing.T) {
	svc, _, _ := startTestService(t)

	pkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicSync,
		payload.New(map[string]any{
			"from": "node-b",
			"groups": map[string]any{
				"workers": "not-a-slice", // should be []any
			},
		}),
	)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	members := svc.GetMembers("workers")
	assert.Nil(t, members)
}

func TestServiceSend_ExitNoExitEvent(t *testing.T) {
	svc, _, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	// Exit message with wrong payload type
	msg := relay.AcquireMessage()
	msg.Topic = topology.TopicEvents
	msg.Payloads = payload.Payloads{payload.New("not an exit event")}
	pkg := relay.NewMessagePackage(p1, testServicePID("local-node"), msg)

	require.NoError(t, svc.Send(pkg))
	time.Sleep(50 * time.Millisecond)

	// Process should still be a member (exit not processed)
	assert.Len(t, svc.GetMembers("workers"), 1)
}

func TestServiceSend_UnknownTopic(t *testing.T) {
	svc, _, _ := startTestService(t)

	pkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), "unknown.topic",
		payload.New(map[string]any{"data": "test"}),
	)

	err := svc.Send(pkg)
	require.NoError(t, err)
	// Should not crash or error
}

func TestServiceSend_MultipleMessages(t *testing.T) {
	svc, _, _ := startTestService(t)

	rp1 := mkNodePID("node-b", "host1", "1")
	rp2 := mkNodePID("node-b", "host1", "2")

	pkg := relay.AcquirePackage()
	pkg.Source = testServicePID("node-b")
	pkg.Target = testServicePID("local-node")

	// Add two join messages in one package
	pkg.AddMessage(pgapi.TopicJoin,
		payload.New(map[string]any{
			"from":  "node-b",
			"group": "workers",
			"pids":  []any{rp1.String()},
		}),
	)
	pkg.AddMessage(pgapi.TopicJoin,
		payload.New(map[string]any{
			"from":  "node-b",
			"group": "managers",
			"pids":  []any{rp2.String()},
		}),
	)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	assert.Len(t, svc.GetMembers("workers"), 1)
	assert.Len(t, svc.GetMembers("managers"), 1)
}

// --- Start() with cluster discovery tests ---

func TestServiceStartWithMembership(t *testing.T) {
	router := newMockRouter()
	topo := newMockTopology()
	logger := zap.NewNop()

	membership := &mockMembership{
		localNode: cluster.NodeInfo{ID: "local-node"},
		nodes: []cluster.NodeInfo{
			{ID: "local-node"},
			{ID: "node-b"},
			{ID: "node-c"},
		},
	}

	svc := NewService(logger, "pg", nil, router, topo, membership, nil, "local-node", nil, nil, nil)

	_, err := svc.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = svc.Stop(context.Background()) }()

	// Allow time for discovery
	time.Sleep(100 * time.Millisecond)

	sends := router.getSends()
	// Should have sent discover to node-b and node-c (but not local-node)
	discoverCount := 0
	for _, s := range sends {
		for _, msg := range s.Messages {
			if msg.Topic == pgapi.TopicDiscover {
				discoverCount++
			}
		}
	}
	assert.Equal(t, 2, discoverCount, "should send discover to 2 remote nodes")
}

func TestServiceStartWithMembershipNoRemoteNodes(t *testing.T) {
	router := newMockRouter()
	topo := newMockTopology()
	logger := zap.NewNop()

	membership := &mockMembership{
		localNode: cluster.NodeInfo{ID: "local-node"},
		nodes: []cluster.NodeInfo{
			{ID: "local-node"}, // only local node
		},
	}

	svc := NewService(logger, "pg", nil, router, topo, membership, nil, "local-node", nil, nil, nil)

	_, err := svc.Start(context.Background())
	require.NoError(t, err)
	defer func() { _ = svc.Stop(context.Background()) }()

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	// No discover messages since no remote nodes
	for _, s := range sends {
		for _, msg := range s.Messages {
			assert.NotEqual(t, pgapi.TopicDiscover, msg.Topic, "should not discover local-only cluster")
		}
	}
}

// --- handleNodeJoinedEvent / handleNodeLeftEvent tests ---

func TestServiceHandleNodeJoinedEvent(t *testing.T) {
	svc, router, _ := startTestService(t)

	router.reset()

	svc.handleNodeJoinedEvent(event.Event{
		Data: cluster.NodeEvent{
			Node: cluster.NodeInfo{ID: "node-new"},
		},
	})

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	found := false
	for _, s := range sends {
		for _, msg := range s.Messages {
			if msg.Topic == pgapi.TopicDiscover {
				found = true
			}
		}
	}
	assert.True(t, found, "should send discover on node joined event")
}

func TestServiceHandleNodeJoinedEventLocalNode(t *testing.T) {
	svc, router, _ := startTestService(t)

	router.reset()

	// Event for local node should be ignored
	svc.handleNodeJoinedEvent(event.Event{
		Data: cluster.NodeEvent{
			Node: cluster.NodeInfo{ID: "local-node"},
		},
	})

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	assert.Empty(t, sends, "should ignore node joined event for local node")
}

func TestServiceHandleNodeJoinedEventWrongDataType(t *testing.T) {
	svc, router, _ := startTestService(t)

	router.reset()

	// Wrong data type should be ignored
	svc.handleNodeJoinedEvent(event.Event{
		Data: "not a NodeEvent",
	})

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	assert.Empty(t, sends, "should ignore event with wrong data type")
}

func TestServiceHandleNodeLeftEvent(t *testing.T) {
	svc, _, _ := startTestService(t)

	// First register a remote node with members
	rp1 := mkNodePID("node-b", "host1", "1")
	svc.submit(func() {
		svc.handleSync("node-b", map[string][]pid.PID{
			"workers": {rp1},
		})
		svc.publishDirty()
	})
	time.Sleep(50 * time.Millisecond)

	require.Len(t, svc.GetMembers("workers"), 1)

	// Fire node left event
	svc.handleNodeLeftEvent(event.Event{
		Data: cluster.NodeEvent{
			Node: cluster.NodeInfo{ID: "node-b"},
		},
	})

	time.Sleep(50 * time.Millisecond)

	assert.Empty(t, svc.GetMembers("workers"), "should remove remote node members on node left")
}

func TestServiceHandleNodeLeftEventLocalNode(t *testing.T) {
	svc, _, _ := startTestService(t)

	// Event for local node should be ignored (should not panic or remove local state)
	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	svc.handleNodeLeftEvent(event.Event{
		Data: cluster.NodeEvent{
			Node: cluster.NodeInfo{ID: "local-node"},
		},
	})

	time.Sleep(50 * time.Millisecond)

	// Local members should still be there
	assert.Len(t, svc.GetMembers("workers"), 1)
}

func TestServiceHandleNodeLeftEventWrongDataType(t *testing.T) {
	svc, _, _ := startTestService(t)

	// Should not panic
	svc.handleNodeLeftEvent(event.Event{
		Data: 42,
	})

	time.Sleep(50 * time.Millisecond)
}

// --- Service context cancellation tests ---

func TestServiceJoinAfterStop(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, func() error { _, err := svc.Start(context.Background()); return err }())
	require.NoError(t, svc.Stop(context.Background()))

	p1 := mkPID("host1", "1")
	err := svc.Join("workers", p1)
	assert.ErrorIs(t, err, ErrServiceStopped)
}

func TestServiceLeaveAfterStop(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, func() error { _, err := svc.Start(context.Background()); return err }())
	require.NoError(t, svc.Stop(context.Background()))

	p1 := mkPID("host1", "1")
	err := svc.Leave("workers", p1)
	assert.ErrorIs(t, err, ErrServiceStopped)
}

func TestServiceGetMembersAfterStop(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, func() error { _, err := svc.Start(context.Background()); return err }())
	require.NoError(t, svc.Stop(context.Background()))

	members := svc.GetMembers("workers")
	assert.Nil(t, members)
}

func TestServiceGetLocalMembersAfterStop(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, func() error { _, err := svc.Start(context.Background()); return err }())
	require.NoError(t, svc.Stop(context.Background()))

	members := svc.GetLocalMembers("workers")
	assert.Nil(t, members)
}

func TestServiceWhichGroupsAfterStop(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, func() error { _, err := svc.Start(context.Background()); return err }())
	require.NoError(t, svc.Stop(context.Background()))

	groups := svc.WhichGroups()
	assert.Nil(t, groups)
}

func TestServiceBroadcastAfterStop(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, func() error { _, err := svc.Start(context.Background()); return err }())
	require.NoError(t, svc.Stop(context.Background()))

	sender := mkPID("host1", "sender")
	_, err := svc.Broadcast(sender, "workers", "hello", nil)
	assert.ErrorIs(t, err, ErrServiceStopped)
}

func TestServiceBroadcastLocalAfterStop(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, func() error { _, err := svc.Start(context.Background()); return err }())
	require.NoError(t, svc.Stop(context.Background()))

	sender := mkPID("host1", "sender")
	_, err := svc.BroadcastLocal(sender, "workers", "hello", nil)
	assert.ErrorIs(t, err, ErrServiceStopped)
}

// --- sendToMembers tests ---

func TestServiceSendToMembersPartialFailure(t *testing.T) {
	svc, router, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")
	p3 := mkPID("host1", "3")
	sender := mkPID("host1", "sender")

	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("workers", p2))
	require.NoError(t, svc.Join("workers", p3))

	// Make router fail
	router.mu.Lock()
	router.sendErr = errors.New("router send failed")
	router.mu.Unlock()

	router.reset()

	// Broadcast through the service event loop
	sent, err := svc.Broadcast(sender, "workers", "hello", nil)
	require.NoError(t, err) // Broadcast itself doesn't fail
	assert.Equal(t, 0, sent, "all sends should have failed")

	time.Sleep(50 * time.Millisecond)

	// Router rejected all sends
	sends := router.getSends()
	assert.Empty(t, sends)
}

func TestServiceSendToMembersEmpty(t *testing.T) {
	svc, router, _ := startTestService(t)

	sender := mkPID("host1", "sender")

	router.reset()

	// Broadcast to nonexistent group — no members
	sent, err := svc.Broadcast(sender, "nonexistent", "hello", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, sent)

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	assert.Empty(t, sends)
}

// --- Event emission tests ---

// monitorEvent is a parsed membership event captured from a monitor's relay topic.
type monitorEvent struct {
	group string
	kind  string
	pids  []pid.PID
}

// collectMonitorEvents extracts membership events delivered to the given monitor
// PID from the recorded router sends. Events arrive via the relay (router.Send)
// as a map matching the eventbus format, carrying a pgapi.MembershipEvent payload.
func collectMonitorEvents(t *testing.T, router *mockRouter, target pid.PID) []monitorEvent {
	t.Helper()
	var events []monitorEvent
	for _, s := range router.getSends() {
		if s.Target != target {
			continue
		}
		for _, msg := range s.Messages {
			if len(msg.Payloads) == 0 {
				continue
			}
			data, ok := msg.Payloads[0].Data().(map[string]any)
			if !ok {
				continue
			}
			kind, _ := data["kind"].(string)
			memberEvt, ok := data["data"].(pgapi.MembershipEvent)
			if !ok {
				continue
			}
			events = append(events, monitorEvent{
				group: memberEvt.Group,
				kind:  kind,
				pids:  memberEvt.PIDs,
			})
		}
	}
	return events
}

// waitForMonitorEvents polls the recorded router sends until at least min events
// for the target are observed or the timeout elapses, then returns what was seen.
func waitForMonitorEvents(t *testing.T, router *mockRouter, target pid.PID, min int) []monitorEvent {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		events := collectMonitorEvents(t, router, target)
		if len(events) >= min {
			return events
		}
		select {
		case <-deadline:
			return events
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestServiceEmitsJoinEvent(t *testing.T) {
	svc, router, _ := startTestService(t)

	// Observe membership events through the relay monitor path.
	monPID := mkPID("host1", "mon")
	result := svc.Events(monPID, "pg.events")
	defer result.Unsubscribe()

	router.reset()

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	events := waitForMonitorEvents(t, router, monPID, 1)
	require.Len(t, events, 1)
	assert.Equal(t, pgapi.MemberJoined, events[0].kind)
	assert.Equal(t, "workers", events[0].group)
	require.Len(t, events[0].pids, 1)
	assert.Equal(t, p1.String(), events[0].pids[0].String())
}

func TestServiceEmitsLeaveEvent(t *testing.T) {
	svc, router, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	monPID := mkPID("host1", "mon")
	result := svc.Events(monPID, "pg.events")
	defer result.Unsubscribe()

	router.reset()

	require.NoError(t, svc.Leave("workers", p1))

	events := waitForMonitorEvents(t, router, monPID, 1)
	require.Len(t, events, 1)
	assert.Equal(t, pgapi.MemberLeft, events[0].kind)
	assert.Equal(t, "workers", events[0].group)
	require.Len(t, events[0].pids, 1)
	assert.Equal(t, p1.String(), events[0].pids[0].String())
}

func TestServiceEmitsEventOnProcessExit(t *testing.T) {
	svc, router, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	monPID := mkPID("host1", "mon")
	result := svc.Events(monPID, "pg.events")
	defer result.Unsubscribe()

	router.reset()

	// Simulate process exit via relay
	exitEvent := &topology.ExitEvent{From: p1}
	msg := relay.AcquireMessage()
	msg.Topic = topology.TopicEvents
	msg.Payloads = payload.Payloads{payload.New(exitEvent)}
	pkg := relay.NewMessagePackage(p1, testServicePID("local-node"), msg)
	require.NoError(t, svc.Send(pkg))

	events := waitForMonitorEvents(t, router, monPID, 1)
	require.Len(t, events, 1)
	assert.Equal(t, pgapi.MemberLeft, events[0].kind)
	assert.Equal(t, "workers", events[0].group)
	require.Len(t, events[0].pids, 1)
	assert.Equal(t, p1.String(), events[0].pids[0].String())
}

func TestServiceMultiJoinExitEmitsDedupedEvents(t *testing.T) {
	svc, router, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	// Join "workers" 3 times and "managers" once
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("managers", p1))

	monPID := mkPID("host1", "mon")
	result := svc.Events(monPID, "pg.events")
	defer result.Unsubscribe()

	router.reset()

	// Simulate process exit
	exitEvent := &topology.ExitEvent{From: p1}
	msg := relay.AcquireMessage()
	msg.Topic = topology.TopicEvents
	msg.Payloads = payload.Payloads{payload.New(exitEvent)}
	pkg := relay.NewMessagePackage(p1, testServicePID("local-node"), msg)
	require.NoError(t, svc.Send(pkg))

	// Should get exactly 2 leave events (one per unique group)
	// Erlang PG semantics: the PID list in the "workers" event should
	// contain p1 three times (one per join occurrence).
	received := waitForMonitorEvents(t, router, monPID, 2)
	require.Len(t, received, 2, "expected 2 leave events")

	groupLeaves := make(map[string][]pid.PID)
	for _, r := range received {
		assert.Equal(t, pgapi.MemberLeft, r.kind)
		groupLeaves[r.group] = r.pids
	}
	assert.Contains(t, groupLeaves, "workers")
	assert.Contains(t, groupLeaves, "managers")
	// "workers" had 3 joins: leave event should carry [p1, p1, p1]
	assert.Len(t, groupLeaves["workers"], 3, "workers leave event should have 3 PIDs (one per join)")
	for _, p := range groupLeaves["workers"] {
		assert.Equal(t, p1.String(), p.String())
	}
	// "managers" had 1 join: leave event should carry [p1]
	assert.Len(t, groupLeaves["managers"], 1, "managers leave event should have 1 PID")
	assert.Equal(t, p1.String(), groupLeaves["managers"][0].String())

	// Verify no extra events beyond the expected 2
	time.Sleep(200 * time.Millisecond)
	assert.Len(t, collectMonitorEvents(t, router, monPID), 2, "no extra leave events expected")
}

func TestServiceEmitsNoEventWithNilBus(t *testing.T) {
	// Service with nil bus should not panic on join/leave
	svc, _, _ := startTestService(t) // startTestService uses nil bus

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Leave("workers", p1))
	// Should not panic — emit*Event delivers only via relay, no bus dependency
}

func TestServiceEmitsMultipleJoinEvents(t *testing.T) {
	svc, router, _ := startTestService(t)

	monPID := mkPID("host1", "mon")
	result := svc.Events(monPID, "pg.events")
	defer result.Unsubscribe()

	router.reset()

	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("workers", p2))

	events := waitForMonitorEvents(t, router, monPID, 2)
	require.Len(t, events, 2, "expected 2 join events")
	for _, e := range events {
		assert.Equal(t, pgapi.MemberJoined, e.kind)
	}
}

func TestServiceEmitsEventsForRemoteJoin(t *testing.T) {
	svc, router, _ := startTestService(t)

	monPID := mkPID("host1", "mon")
	result := svc.Events(monPID, "pg.events")
	defer result.Unsubscribe()

	router.reset()

	// Simulate remote join via relay package
	rp1 := mkNodePID("node-b", "host1", "1")
	pkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicJoin,
		payload.New(map[string]any{
			"from":  "node-b",
			"group": "workers",
			"pids":  []any{rp1.String()},
		}),
	)
	require.NoError(t, svc.Send(pkg))

	events := waitForMonitorEvents(t, router, monPID, 1)
	require.Len(t, events, 1)
	assert.Equal(t, pgapi.MemberJoined, events[0].kind)
	assert.Equal(t, "workers", events[0].group)
	require.Len(t, events[0].pids, 1)
	assert.Equal(t, rp1.String(), events[0].pids[0].String())
}

func TestServiceEmitsEventsForRemoteLeave(t *testing.T) {
	svc, router, _ := startTestService(t)

	// First join remotely
	rp1 := mkNodePID("node-b", "host1", "1")
	joinPkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicJoin,
		payload.New(map[string]any{
			"from":  "node-b",
			"group": "workers",
			"pids":  []any{rp1.String()},
		}),
	)
	require.NoError(t, svc.Send(joinPkg))
	time.Sleep(50 * time.Millisecond)

	monPID := mkPID("host1", "mon")
	result := svc.Events(monPID, "pg.events")
	defer result.Unsubscribe()

	router.reset()

	// Remote leave
	leavePkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicLeave,
		payload.New(map[string]any{
			"from":   "node-b",
			"pids":   []any{rp1.String()},
			"groups": []any{"workers"},
		}),
	)
	require.NoError(t, svc.Send(leavePkg))

	events := waitForMonitorEvents(t, router, monPID, 1)
	require.Len(t, events, 1)
	assert.Equal(t, pgapi.MemberLeft, events[0].kind)
	assert.Equal(t, "workers", events[0].group)
}

func TestServiceRemoteLeaveNoSpuriousEventsForNonMemberGroups(t *testing.T) {
	svc, router, _ := startTestService(t)

	rp1 := mkNodePID("node-b", "host1", "1")

	// Join rp1 to "workers" only (not "managers")
	joinPkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicJoin,
		payload.New(map[string]any{
			"from":  "node-b",
			"group": "workers",
			"pids":  []any{rp1.String()},
		}),
	)
	require.NoError(t, svc.Send(joinPkg))
	time.Sleep(50 * time.Millisecond)

	// Observe ALL leave events via the relay monitor path
	monPID := mkPID("host1", "mon")
	result := svc.Events(monPID, "pg.events")
	defer result.Unsubscribe()

	router.reset()

	// Send leave for both "workers" and "managers" — rp1 is only in "workers"
	leavePkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicLeave,
		payload.New(map[string]any{
			"from":   "node-b",
			"pids":   []any{rp1.String()},
			"groups": []any{"workers", "managers"},
		}),
	)
	require.NoError(t, svc.Send(leavePkg))

	// Should receive exactly one leave event for "workers" and none for "managers"
	events := waitForMonitorEvents(t, router, monPID, 1)
	time.Sleep(200 * time.Millisecond)
	events = collectMonitorEvents(t, router, monPID)
	require.Len(t, events, 1, "expected exactly one leave event for workers, no spurious managers event")
	assert.Equal(t, pgapi.MemberLeft, events[0].kind)
	assert.Equal(t, "workers", events[0].group)
}

func TestServiceRemoteLeaveDoesNotCorruptOtherNodeMembers(t *testing.T) {
	svc, router, _ := startTestService(t)

	rp1 := mkNodePID("node-b", "host1", "1")
	rp2 := mkNodePID("node-c", "host1", "2")

	// Join rp1 (node-b) to "workers", rp2 (node-c) to "workers" and "managers"
	for _, jp := range []struct {
		from  string
		group string
		pid   pid.PID
	}{
		{"node-b", "workers", rp1},
		{"node-c", "workers", rp2},
		{"node-c", "managers", rp2},
	} {
		pkg := relay.NewPackage(testServicePID(jp.from), testServicePID("local-node"), pgapi.TopicJoin,
			payload.New(map[string]any{
				"from":  jp.from,
				"group": jp.group,
				"pids":  []any{jp.pid.String()},
			}),
		)
		require.NoError(t, svc.Send(pkg))
	}
	time.Sleep(50 * time.Millisecond)

	assert.Len(t, svc.GetMembers("workers"), 2)
	assert.Len(t, svc.GetMembers("managers"), 1)

	monPID := mkPID("host1", "mon")
	result := svc.Events(monPID, "pg.events")
	defer result.Unsubscribe()

	router.reset()

	// Leave rp1 from "workers" and "managers" on node-b.
	// rp1 was never in "managers", so rp2's membership must not be affected.
	leavePkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicLeave,
		payload.New(map[string]any{
			"from":   "node-b",
			"pids":   []any{rp1.String()},
			"groups": []any{"workers", "managers"},
		}),
	)
	require.NoError(t, svc.Send(leavePkg))

	// Should get exactly one leave event for "workers", none for "managers"
	events := waitForMonitorEvents(t, router, monPID, 1)
	time.Sleep(200 * time.Millisecond)
	events = collectMonitorEvents(t, router, monPID)
	require.Len(t, events, 1, "expected exactly one leave event for workers")
	assert.Equal(t, "workers", events[0].group)

	// rp2 should still be in both groups
	workers := svc.GetMembers("workers")
	require.Len(t, workers, 1)
	assert.Equal(t, rp2.String(), workers[0].String())

	managers := svc.GetMembers("managers")
	require.Len(t, managers, 1)
	assert.Equal(t, rp2.String(), managers[0].String())
}

// --- JoinGroups tests ---

func TestServiceJoinGroups(t *testing.T) {
	svc, _, topo := startTestService(t)

	p1 := mkPID("host1", "1")
	err := svc.JoinGroups([]string{"workers", "managers"}, p1)
	require.NoError(t, err)

	assert.Len(t, svc.GetMembers("workers"), 1)
	assert.Len(t, svc.GetMembers("managers"), 1)

	// Should be monitored
	assert.True(t, topo.isMonitored(p1))
}

func TestServiceJoinGroupsEmpty(t *testing.T) {
	svc, _, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	err := svc.JoinGroups([]string{}, p1)
	require.NoError(t, err)

	assert.Empty(t, svc.WhichGroups())
}

func TestServiceJoinGroupsAfterStop(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, func() error { _, err := svc.Start(context.Background()); return err }())
	require.NoError(t, svc.Stop(context.Background()))

	p1 := mkPID("host1", "1")
	err := svc.JoinGroups([]string{"workers"}, p1)
	assert.ErrorIs(t, err, ErrServiceStopped)
}

func TestServiceJoinGroupsEmitsEvents(t *testing.T) {
	svc, router, _ := startTestService(t)

	monPID := mkPID("host1", "mon")
	result := svc.Events(monPID, "pg.events")
	defer result.Unsubscribe()

	router.reset()

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.JoinGroups([]string{"workers", "managers"}, p1))

	events := waitForMonitorEvents(t, router, monPID, 2)
	require.Len(t, events, 2, "expected 2 join events")
	for _, e := range events {
		assert.Equal(t, pgapi.MemberJoined, e.kind)
	}
}

func TestServiceJoinGroupsDuplicateNewGroupDoesNotTripMaxGroups(t *testing.T) {
	cfg := &pgapi.Config{MaxGroups: 1}
	cfg.InitDefaults()

	router := newMockRouter()
	topo := newMockTopology()
	svc := NewService(zap.NewNop(), "pg", cfg, router, topo, nil, nil, "local-node", nil, nil, nil)
	_, err := svc.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = svc.Stop(context.Background())
	})

	p1 := mkPID("host1", "1")
	err = svc.JoinGroups([]string{"workers", "workers"}, p1)
	require.NoError(t, err)

	assert.Len(t, svc.WhichGroups(), 1)
	assert.Len(t, svc.GetMembers("workers"), 2)
}

func TestServiceJoinGroupsDuplicateRespectsMaxMembersPerGroup(t *testing.T) {
	cfg := &pgapi.Config{MaxMembersPerGroup: 2}
	cfg.InitDefaults()

	router := newMockRouter()
	topo := newMockTopology()
	svc := NewService(zap.NewNop(), "pg", cfg, router, topo, nil, nil, "local-node", nil, nil, nil)
	_, err := svc.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = svc.Stop(context.Background())
	})

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	err = svc.JoinGroups([]string{"workers", "workers"}, p1)
	assert.ErrorIs(t, err, pgapi.ErrMaxMembersReached)
	assert.Len(t, svc.GetMembers("workers"), 1)
}

// --- LeaveGroups tests ---

func TestServiceLeaveGroups(t *testing.T) {
	svc, _, topo := startTestService(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.JoinGroups([]string{"workers", "managers"}, p1))

	err := svc.LeaveGroups([]string{"workers", "managers"}, p1)
	require.NoError(t, err)

	assert.Empty(t, svc.GetMembers("workers"))
	assert.Empty(t, svc.GetMembers("managers"))

	// Should be demonitored
	assert.False(t, topo.isMonitored(p1))
}

func TestServiceLeaveGroupsPartial(t *testing.T) {
	svc, _, topo := startTestService(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.JoinGroups([]string{"workers", "managers"}, p1))

	// Leave only workers
	err := svc.LeaveGroups([]string{"workers"}, p1)
	require.NoError(t, err)

	assert.Empty(t, svc.GetMembers("workers"))
	assert.Len(t, svc.GetMembers("managers"), 1)

	// Still monitored because still in managers
	assert.True(t, topo.isMonitored(p1))
}

func TestServiceLeaveGroupsNotJoined(t *testing.T) {
	svc, _, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	err := svc.LeaveGroups([]string{"workers"}, p1)
	assert.ErrorIs(t, err, ErrNotJoined)
}

func TestServiceLeaveGroupsBestEffort(t *testing.T) {
	svc, _, topo := startTestService(t)

	p1 := mkPID("host1", "1")
	// Join groups A and C, but NOT B
	require.NoError(t, svc.Join("groupA", p1))
	require.NoError(t, svc.Join("groupC", p1))

	// Leave A, B, C — B is not joined, but A and C should still be left
	err := svc.LeaveGroups([]string{"groupA", "groupB", "groupC"}, p1)
	require.NoError(t, err, "should succeed because A and C were left")

	assert.Empty(t, svc.GetMembers("groupA"), "groupA should be empty")
	assert.Empty(t, svc.GetMembers("groupC"), "groupC should be empty")

	// Process has no more groups, should be demonitored
	assert.False(t, topo.isMonitored(p1))
}

func TestServiceLeaveGroupsAllNotJoined(t *testing.T) {
	svc, _, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	// Process is not joined to any of these groups
	err := svc.LeaveGroups([]string{"groupX", "groupY"}, p1)
	assert.ErrorIs(t, err, ErrNotJoined, "should return ErrNotJoined when process was not in ANY group")
}

func TestServiceLeaveGroupsBestEffortKeepsMonitor(t *testing.T) {
	svc, _, topo := startTestService(t)

	p1 := mkPID("host1", "1")
	// Join A, B, C
	require.NoError(t, svc.JoinGroups([]string{"a", "b", "c"}, p1))

	// Leave only A and a non-existent group
	err := svc.LeaveGroups([]string{"a", "nonexistent"}, p1)
	require.NoError(t, err, "should succeed because 'a' was left")

	assert.Empty(t, svc.GetMembers("a"))
	assert.Len(t, svc.GetMembers("b"), 1, "b should still have the member")
	assert.Len(t, svc.GetMembers("c"), 1, "c should still have the member")

	// Still in b and c, so should still be monitored
	assert.True(t, topo.isMonitored(p1))
}

func TestServiceMultiJoinExitBroadcast(t *testing.T) {
	svc, router, _ := startTestService(t)

	// Register a remote node so broadcasts have a target
	rp := mkNodePID("node-b", "host1", "99")
	joinPkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicJoin,
		payload.New(map[string]any{
			"from":  "node-b",
			"group": "other",
			"pids":  []any{rp.String()},
		}),
	)
	require.NoError(t, svc.Send(joinPkg))
	time.Sleep(50 * time.Millisecond)

	p1 := mkPID("host1", "1")
	// Join "workers" twice
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("workers", p1))
	assert.Len(t, svc.GetMembers("workers"), 2)

	router.reset()

	// Simulate process exit
	exitEvent := &topology.ExitEvent{From: p1}
	msg := relay.AcquireMessage()
	msg.Topic = topology.TopicEvents
	msg.Payloads = payload.Payloads{payload.New(exitEvent)}
	pkg := relay.NewMessagePackage(p1, testServicePID("local-node"), msg)
	require.NoError(t, svc.Send(pkg))

	time.Sleep(100 * time.Millisecond)

	// Group should be empty after exit
	assert.Empty(t, svc.GetMembers("workers"))

	// Verify the broadcast leave sent to remote nodes lists the PID twice
	// under the "workers" group entry (one slot per multi-join occurrence).
	sends := router.getSends()
	leaveFound := false
	for _, s := range sends {
		for _, m := range s.Messages {
			if m.Topic == pgapi.TopicLeave {
				leaveFound = true
				data, ok := m.Payloads[0].Data().(map[string]any)
				require.True(t, ok)
				leavesMap, ok := data["leaves"].(map[string][]string)
				require.True(t, ok, "leaves payload field missing or wrong type")
				workersPids := leavesMap["workers"]
				assert.Len(t, workersPids, 2,
					"broadcastLeave should list 'workers' PID twice for multi-join exit")
			}
		}
	}
	assert.True(t, leaveFound, "should have sent at least one leave broadcast")
}

func TestServiceLeaveGroupsAfterStop(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, func() error { _, err := svc.Start(context.Background()); return err }())
	require.NoError(t, svc.Stop(context.Background()))

	p1 := mkPID("host1", "1")
	err := svc.LeaveGroups([]string{"workers"}, p1)
	assert.ErrorIs(t, err, ErrServiceStopped)
}

// --- WhichLocalGroups tests ---

func TestServiceWhichLocalGroups(t *testing.T) {
	svc, _, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("managers", p1))

	groups := svc.WhichLocalGroups()
	assert.Len(t, groups, 2)
}

func TestServiceWhichLocalGroupsEmpty(t *testing.T) {
	svc, _, _ := startTestService(t)

	groups := svc.WhichLocalGroups()
	assert.Empty(t, groups)
}

func TestServiceWhichLocalGroupsExcludesRemote(t *testing.T) {
	svc, _, _ := startTestService(t)

	// Add remote members only
	rp1 := mkNodePID("node-b", "host1", "1")
	pkg := relay.NewPackage(testServicePID("node-b"), testServicePID("local-node"), pgapi.TopicJoin,
		payload.New(map[string]any{
			"from":  "node-b",
			"group": "remote-only",
			"pids":  []any{rp1.String()},
		}),
	)
	require.NoError(t, svc.Send(pkg))
	time.Sleep(50 * time.Millisecond)

	localGroups := svc.WhichLocalGroups()
	assert.Empty(t, localGroups)

	allGroups := svc.WhichGroups()
	assert.Len(t, allGroups, 1)
}

func TestServiceWhichLocalGroupsAfterStop(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, func() error { _, err := svc.Start(context.Background()); return err }())
	require.NoError(t, svc.Stop(context.Background()))

	groups := svc.WhichLocalGroups()
	assert.Nil(t, groups)
}

// --- Monitor tests ---

func TestServiceMonitor(t *testing.T) {
	svc, router, _ := startTestService(t)

	// Join some members first
	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("workers", p2))

	// Monitor the group
	monitorPID := mkPID("host1", "monitor")
	result := svc.Monitor("workers", monitorPID, "pg.event")

	// Should get current members as snapshot
	assert.Len(t, result.Members, 2)
	assert.NotNil(t, result.Unsubscribe)

	router.reset()

	// Now join a new member — should be delivered to monitor
	p3 := mkPID("host1", "3")
	require.NoError(t, svc.Join("workers", p3))

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	// Monitor should have received a join event via relay
	found := false
	for _, s := range sends {
		if s.Target == monitorPID {
			found = true
		}
	}
	assert.True(t, found, "monitor should receive join event")

	// Unsubscribe
	result.Unsubscribe()
	time.Sleep(50 * time.Millisecond)
}

func TestServiceMonitorEmptyGroup(t *testing.T) {
	svc, _, _ := startTestService(t)

	monitorPID := mkPID("host1", "monitor")
	result := svc.Monitor("nonexistent", monitorPID, "pg.event")

	assert.Nil(t, result.Members)
	assert.NotNil(t, result.Unsubscribe)
	result.Unsubscribe()
}

func TestServiceMonitorUnsubscribe(t *testing.T) {
	svc, router, _ := startTestService(t)

	monitorPID := mkPID("host1", "monitor")
	result := svc.Monitor("workers", monitorPID, "pg.event")

	// Unsubscribe immediately
	result.Unsubscribe()
	time.Sleep(50 * time.Millisecond)

	router.reset()

	// New join should not be delivered to monitor
	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))
	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	for _, s := range sends {
		assert.NotEqual(t, monitorPID, s.Target, "unsubscribed monitor should not receive events")
	}
}

// --- Events tests ---

func TestServiceEvents(t *testing.T) {
	svc, _, _ := startTestService(t)

	// Set up some groups first
	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("managers", p2))

	// Subscribe to events
	eventsPID := mkPID("host1", "events")
	result := svc.Events(eventsPID, "pg.event")

	// Should get snapshot of all groups
	assert.Len(t, result.Groups, 2)
	assert.Len(t, result.Groups["workers"], 1)
	assert.Len(t, result.Groups["managers"], 1)
	assert.NotNil(t, result.Unsubscribe)

	result.Unsubscribe()
}

func TestServiceEventsEmpty(t *testing.T) {
	svc, _, _ := startTestService(t)

	eventsPID := mkPID("host1", "events")
	result := svc.Events(eventsPID, "pg.event")

	assert.Empty(t, result.Groups)
	assert.NotNil(t, result.Unsubscribe)
	result.Unsubscribe()
}

func TestServiceEventsReceivesAllGroupEvents(t *testing.T) {
	svc, router, _ := startTestService(t)

	eventsPID := mkPID("host1", "events")
	result := svc.Events(eventsPID, "pg.event")

	router.reset()

	// Join two different groups
	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("managers", p1))

	time.Sleep(100 * time.Millisecond)

	sends := router.getSends()
	eventCount := 0
	for _, s := range sends {
		if s.Target == eventsPID {
			eventCount++
		}
	}
	assert.Equal(t, 2, eventCount, "events subscriber should receive events for all groups")

	result.Unsubscribe()
}

// --- RCU snapshot tests ---

func TestServiceRCUSnapshot(t *testing.T) {
	svc, _, _ := startTestService(t)

	// Before any joins, snapshot should be empty but not nil
	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	// GetMembers uses RCU snapshot
	members := svc.GetMembers("workers")
	require.Len(t, members, 1)
	assert.Equal(t, p1.String(), members[0].String())

	// WhichGroups uses RCU snapshot
	groups := svc.WhichGroups()
	assert.Len(t, groups, 1)

	// GetLocalMembers uses RCU snapshot
	local := svc.GetLocalMembers("workers")
	assert.Len(t, local, 1)
}

func TestServiceRCUSnapshotConsistency(t *testing.T) {
	svc, _, _ := startTestService(t)

	// Join multiple processes rapidly
	for i := 0; i < 10; i++ {
		p := mkPID("host1", string(rune('0'+i)))
		require.NoError(t, svc.Join("workers", p))
	}

	// Read snapshot — should be consistent
	members := svc.GetMembers("workers")
	assert.Len(t, members, 10)
	groups := svc.WhichGroups()
	assert.Len(t, groups, 1)
}

// --- diffPIDs tests ---

func TestDiffPIDs(t *testing.T) {
	p1 := mkPID("h", "1")
	p2 := mkPID("h", "2")
	p3 := mkPID("h", "3")

	t.Run("empty a", func(t *testing.T) {
		result := diffPIDs(nil, []pid.PID{p1})
		assert.Nil(t, result)
	})

	t.Run("empty b", func(t *testing.T) {
		result := diffPIDs([]pid.PID{p1, p2}, nil)
		assert.Len(t, result, 2)
	})

	t.Run("identical", func(t *testing.T) {
		result := diffPIDs([]pid.PID{p1, p2}, []pid.PID{p1, p2})
		assert.Empty(t, result)
	})

	t.Run("a has extra", func(t *testing.T) {
		result := diffPIDs([]pid.PID{p1, p2, p3}, []pid.PID{p1})
		assert.Len(t, result, 2)
		strs := make([]string, len(result))
		for i, p := range result {
			strs[i] = p.String()
		}
		assert.Contains(t, strs, p2.String())
		assert.Contains(t, strs, p3.String())
	})

	t.Run("b has extra", func(t *testing.T) {
		result := diffPIDs([]pid.PID{p1}, []pid.PID{p1, p2, p3})
		assert.Empty(t, result)
	})

	t.Run("multiplicity", func(t *testing.T) {
		// a has p1 x3, b has p1 x1 → result should have p1 x2
		result := diffPIDs([]pid.PID{p1, p1, p1}, []pid.PID{p1})
		assert.Len(t, result, 2)
		for _, p := range result {
			assert.Equal(t, p1.String(), p.String())
		}
	})

	t.Run("multiplicity both sides", func(t *testing.T) {
		// a has p1 x3, p2 x1; b has p1 x2 → result should have p1 x1, p2 x1
		result := diffPIDs([]pid.PID{p1, p1, p1, p2}, []pid.PID{p1, p1})
		assert.Len(t, result, 2)
	})
}

// --- Differential sync tests ---

func TestServiceSyncDifferentialNoSpuriousEvents(t *testing.T) {
	svc, router, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")

	// Set up initial remote state by simulating a sync
	remoteNode := pid.NodeID("remote-1")
	initialGroups := map[string][]pid.PID{
		"workers": {p1, p2},
	}
	svc.submit(func() {
		svc.handleSync(remoteNode, initialGroups)
		svc.publishDirty()
	})
	time.Sleep(50 * time.Millisecond)

	// Set up a monitor to capture events
	monPID := mkPID("host1", "mon")
	result := svc.Monitor("workers", monPID, "pg.monitor")
	assert.Len(t, result.Members, 2)
	defer result.Unsubscribe()

	router.reset()

	// Sync again with identical state — should produce NO events
	svc.submit(func() {
		svc.handleSync(remoteNode, initialGroups)
		svc.publishDirty()
	})
	time.Sleep(100 * time.Millisecond)

	// Check that no monitor events were delivered (no spurious join/leave)
	sends := router.getSends()
	monitorEvents := 0
	for _, s := range sends {
		if s.Target == monPID {
			monitorEvents++
		}
	}
	assert.Equal(t, 0, monitorEvents, "identical sync should produce no monitor events")
}

func TestServiceSyncDifferentialAddsNewPIDs(t *testing.T) {
	svc, router, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")
	p3 := mkPID("host1", "3")

	remoteNode := pid.NodeID("remote-1")

	// Initial sync with p1 only
	svc.submit(func() {
		svc.handleSync(remoteNode, map[string][]pid.PID{
			"workers": {p1},
		})
		svc.publishDirty()
	})
	time.Sleep(50 * time.Millisecond)

	// Monitor the group
	monPID := mkPID("host1", "mon")
	result := svc.Monitor("workers", monPID, "pg.monitor")
	assert.Len(t, result.Members, 1)
	defer result.Unsubscribe()

	router.reset()

	// Sync with p1, p2, p3 — should emit join for p2 and p3 only
	svc.submit(func() {
		svc.handleSync(remoteNode, map[string][]pid.PID{
			"workers": {p1, p2, p3},
		})
		svc.publishDirty()
	})
	time.Sleep(100 * time.Millisecond)

	sends := router.getSends()
	var joinPIDs []pid.PID
	for _, s := range sends {
		if s.Target == monPID && len(s.Messages) > 0 {
			for _, msg := range s.Messages {
				if len(msg.Payloads) > 0 {
					if data, ok := msg.Payloads[0].Data().(map[string]any); ok {
						if kind, _ := data["kind"].(string); kind == pgapi.MemberJoined {
							if evt, ok := data["data"].(pgapi.MembershipEvent); ok {
								joinPIDs = append(joinPIDs, evt.PIDs...)
							}
						}
					}
				}
			}
		}
	}

	assert.Len(t, joinPIDs, 2, "should emit join for 2 new PIDs")
	joinStrs := make([]string, len(joinPIDs))
	for i, p := range joinPIDs {
		joinStrs[i] = p.String()
	}
	assert.Contains(t, joinStrs, p2.String())
	assert.Contains(t, joinStrs, p3.String())
}

func TestServiceSyncDifferentialRemovesPIDs(t *testing.T) {
	svc, router, _ := startTestService(t)

	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")

	remoteNode := pid.NodeID("remote-1")

	// Initial sync with p1 and p2
	svc.submit(func() {
		svc.handleSync(remoteNode, map[string][]pid.PID{
			"workers": {p1, p2},
		})
		svc.publishDirty()
	})
	time.Sleep(50 * time.Millisecond)

	// Monitor the group
	monPID := mkPID("host1", "mon")
	result := svc.Monitor("workers", monPID, "pg.monitor")
	assert.Len(t, result.Members, 2)
	defer result.Unsubscribe()

	router.reset()

	// Sync with p1 only — should emit leave for p2
	svc.submit(func() {
		svc.handleSync(remoteNode, map[string][]pid.PID{
			"workers": {p1},
		})
		svc.publishDirty()
	})
	time.Sleep(100 * time.Millisecond)

	sends := router.getSends()
	var leavePIDs []pid.PID
	for _, s := range sends {
		if s.Target == monPID && len(s.Messages) > 0 {
			for _, msg := range s.Messages {
				if len(msg.Payloads) > 0 {
					if data, ok := msg.Payloads[0].Data().(map[string]any); ok {
						if kind, _ := data["kind"].(string); kind == pgapi.MemberLeft {
							if evt, ok := data["data"].(pgapi.MembershipEvent); ok {
								leavePIDs = append(leavePIDs, evt.PIDs...)
							}
						}
					}
				}
			}
		}
	}

	assert.Len(t, leavePIDs, 1, "should emit leave for 1 removed PID")
	assert.Equal(t, p2.String(), leavePIDs[0].String())
}

func TestServiceSyncDifferentialRemovesGroup(t *testing.T) {
	svc, router, _ := startTestService(t)

	p1 := mkPID("host1", "1")

	remoteNode := pid.NodeID("remote-1")

	// Initial sync with "workers" group
	svc.submit(func() {
		svc.handleSync(remoteNode, map[string][]pid.PID{
			"workers": {p1},
		})
		svc.publishDirty()
	})
	time.Sleep(50 * time.Millisecond)

	// Monitor all events via Events
	monPID := mkPID("host1", "mon")
	eventsResult := svc.Events(monPID, "pg.events")
	assert.Len(t, eventsResult.Groups, 1)
	defer eventsResult.Unsubscribe()

	router.reset()

	// Sync with empty groups — should emit leave for "workers"
	svc.submit(func() {
		svc.handleSync(remoteNode, map[string][]pid.PID{})
		svc.publishDirty()
	})
	time.Sleep(100 * time.Millisecond)

	sends := router.getSends()
	leaveCount := 0
	for _, s := range sends {
		if s.Target == monPID && len(s.Messages) > 0 {
			for _, msg := range s.Messages {
				if len(msg.Payloads) > 0 {
					if data, ok := msg.Payloads[0].Data().(map[string]any); ok {
						if kind, _ := data["kind"].(string); kind == pgapi.MemberLeft {
							leaveCount++
						}
					}
				}
			}
		}
	}
	assert.Equal(t, 1, leaveCount, "should emit exactly 1 leave event for removed group")
}

// --- Synchronous demonitor tests ---

func TestServiceMonitorUnsubscribeSynchronous(t *testing.T) {
	svc, router, _ := startTestService(t)

	// Monitor a group
	monPID := mkPID("host1", "mon")
	result := svc.Monitor("workers", monPID, "pg.monitor")
	assert.NotNil(t, result.Unsubscribe)

	// Unsubscribe synchronously
	result.Unsubscribe()

	router.reset()

	// Join after unsubscribe — should NOT receive any events
	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	time.Sleep(100 * time.Millisecond)

	sends := router.getSends()
	monitorEvents := 0
	for _, s := range sends {
		if s.Target == monPID {
			monitorEvents++
		}
	}
	assert.Equal(t, 0, monitorEvents, "after synchronous unsubscribe, no events should be received")
}

func TestServiceEventsUnsubscribeSynchronous(t *testing.T) {
	svc, router, _ := startTestService(t)

	// Subscribe to all events
	monPID := mkPID("host1", "mon")
	result := svc.Events(monPID, "pg.events")
	assert.NotNil(t, result.Unsubscribe)

	// Unsubscribe synchronously
	result.Unsubscribe()

	router.reset()

	// Join after unsubscribe — should NOT receive any events
	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	time.Sleep(100 * time.Millisecond)

	sends := router.getSends()
	monitorEvents := 0
	for _, s := range sends {
		if s.Target == monPID {
			monitorEvents++
		}
	}
	assert.Equal(t, 0, monitorEvents, "after synchronous events unsubscribe, no events should be received")
}

// --- Monitor caller death cleanup tests ---

func TestServiceMonitorCallerDeathCleansUp(t *testing.T) {
	svc, router, _ := startTestService(t)

	// Process A monitors group "workers"
	processA := mkPID("host1", "procA")
	result := svc.Monitor("workers", processA, "pg.monitor")
	assert.NotNil(t, result.Unsubscribe)

	// Join a process so there's something to monitor
	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))
	time.Sleep(50 * time.Millisecond)

	router.reset()

	// Simulate processA dying (process exit event)
	exitEvent := &topology.ExitEvent{From: processA}
	msg := relay.AcquireMessage()
	msg.Topic = topology.TopicEvents
	msg.Payloads = payload.Payloads{payload.New(exitEvent)}
	pkg := relay.NewMessagePackage(processA, testServicePID("local-node"), msg)
	require.NoError(t, svc.Send(pkg))

	time.Sleep(50 * time.Millisecond)

	router.reset()

	// Join another process — processA should NOT receive the event (cleaned up)
	p2 := mkPID("host1", "2")
	require.NoError(t, svc.Join("workers", p2))

	time.Sleep(100 * time.Millisecond)

	sends := router.getSends()
	for _, s := range sends {
		if s.Target == processA {
			t.Fatal("dead process should not receive monitor events after cleanup")
		}
	}
}

func TestServiceEventsCallerDeathCleansUp(t *testing.T) {
	svc, router, _ := startTestService(t)

	// Process A subscribes to all events
	processA := mkPID("host1", "procA")
	result := svc.Events(processA, "pg.events")
	assert.NotNil(t, result.Unsubscribe)

	router.reset()

	// Simulate processA dying
	exitEvent := &topology.ExitEvent{From: processA}
	msg := relay.AcquireMessage()
	msg.Topic = topology.TopicEvents
	msg.Payloads = payload.Payloads{payload.New(exitEvent)}
	pkg := relay.NewMessagePackage(processA, testServicePID("local-node"), msg)
	require.NoError(t, svc.Send(pkg))

	time.Sleep(50 * time.Millisecond)

	router.reset()

	// Join a process — processA should NOT receive events (cleaned up)
	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	time.Sleep(100 * time.Millisecond)

	sends := router.getSends()
	for _, s := range sends {
		if s.Target == processA {
			t.Fatal("dead process should not receive wildcard events after cleanup")
		}
	}
}

func TestServiceMonitorCallerDeathDemonitors(t *testing.T) {
	svc, _, topo := startTestService(t)

	// Process A monitors group "workers" but is NOT a group member
	processA := mkPID("host1", "procA")
	result := svc.Monitor("workers", processA, "pg.monitor")
	assert.NotNil(t, result.Unsubscribe)

	time.Sleep(50 * time.Millisecond)

	// processA should be monitored by topology
	assert.True(t, topo.isMonitored(processA), "subscriber should be monitored via topology")

	// Unsubscribe — processA has no group memberships and no more subscriptions
	result.Unsubscribe()

	time.Sleep(50 * time.Millisecond)

	// processA should no longer be monitored
	assert.False(t, topo.isMonitored(processA), "subscriber should be demonitored after unsubscribe")
}

func TestServiceMonitorUnsubscribeKeepsMonitorForGroupMember(t *testing.T) {
	svc, _, topo := startTestService(t)

	// Process A joins a group AND monitors it
	processA := mkPID("host1", "procA")
	require.NoError(t, svc.Join("workers", processA))

	result := svc.Monitor("workers", processA, "pg.monitor")
	assert.NotNil(t, result.Unsubscribe)

	time.Sleep(50 * time.Millisecond)

	assert.True(t, topo.isMonitored(processA))

	// Unsubscribe from monitor — should STILL be monitored (still a group member)
	result.Unsubscribe()

	time.Sleep(50 * time.Millisecond)

	assert.True(t, topo.isMonitored(processA), "should still be monitored because process is still a group member")
}

// --- Helper method tests ---

func TestServiceHasMonitorSubscriptions(t *testing.T) {
	svc, _, _ := startTestService(t)

	processA := mkPID("host1", "procA")

	// No subscriptions initially
	done := make(chan bool, 1)
	svc.submit(func() {
		done <- svc.hasMonitorSubscriptions(processA)
	})
	assert.False(t, <-done)

	// Add a monitor subscription
	result := svc.Monitor("workers", processA, "pg.monitor")

	svc.submit(func() {
		done <- svc.hasMonitorSubscriptions(processA)
	})
	assert.True(t, <-done)

	// Unsubscribe
	result.Unsubscribe()

	svc.submit(func() {
		done <- svc.hasMonitorSubscriptions(processA)
	})
	assert.False(t, <-done)
}

func TestServiceHasGroupMemberships(t *testing.T) {
	svc, _, _ := startTestService(t)

	processA := mkPID("host1", "procA")

	// No memberships initially
	done := make(chan bool, 1)
	svc.submit(func() {
		done <- svc.hasGroupMemberships(processA)
	})
	assert.False(t, <-done)

	// Join a group
	require.NoError(t, svc.Join("workers", processA))

	svc.submit(func() {
		done <- svc.hasGroupMemberships(processA)
	})
	assert.True(t, <-done)

	// Leave the group
	require.NoError(t, svc.Leave("workers", processA))

	svc.submit(func() {
		done <- svc.hasGroupMemberships(processA)
	})
	assert.False(t, <-done)
}

// TestSubmitDropsAtCapacityWithoutBlocking proves submit() honors the
// bounded-drop contract: once the action channel saturates at its hard
// cap, submit() returns false promptly and records
// pg_queue_dropped_total{reason="full"} instead of blocking the caller.
// The event loop is stalled on a gate so nothing drains the channel.
func TestSubmitDropsAtCapacityWithoutBlocking(t *testing.T) {
	router := newMockRouter()
	topo := newMockTopology()
	rec := telemetrytest.NewRecorder()

	cfg := &pgapi.Config{ActionQueueSize: 4, ActionQueueMaxSize: 8}
	cfg.InitDefaults()
	require.NoError(t, cfg.Validate())

	svc := NewService(zap.NewNop(), "pg", cfg, router, topo, nil, nil, "local-node", rec, nil, nil)
	_, err := svc.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })

	// Stall the event loop: the first submitted action blocks on gate,
	// so the loop consumes nothing else and the channel fills up.
	gate := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(gate)
		}
	}()
	require.True(t, svc.submit(func() { <-gate }))

	// Wait until the loop has picked up the gate action; the channel is
	// then empty again and we can deterministically fill it to capacity.
	require.Eventually(t, func() bool {
		return len(svc.actions) == 0
	}, time.Second, time.Millisecond)

	cap := cap(svc.actions)
	require.Greater(t, cap, 0)

	// Fill the channel to its hard cap.
	for i := 0; i < cap; i++ {
		require.True(t, svc.submit(func() {}), "submit %d should buffer", i)
	}

	// The channel is now full and the loop is stalled. One more submit
	// must drop without blocking. A sentinel goroutine detects a block.
	result := make(chan bool, 1)
	go func() {
		result <- svc.submit(func() {})
	}()

	select {
	case ok := <-result:
		require.False(t, ok, "submit at capacity must return false (drop)")
	case <-time.After(2 * time.Second):
		t.Fatal("submit() blocked at capacity instead of dropping")
	}

	// The drop branch must be reachable and counted.
	require.Equal(t, float64(1),
		rec.CounterValue("pg_queue_dropped_total", metrics.Labels{"pg": "pg", "reason": "full"}),
		"drop at capacity must record pg_queue_dropped_total{reason=full}")

	close(gate)
	released = true
}
