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
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/eventbus"
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

func newTestService() (*Service, *mockRouter, *mockTopology) {
	router := newMockRouter()
	topo := newMockTopology()
	logger := zap.NewNop()
	svc := NewService(logger, router, topo, nil, nil, "local-node")
	return svc, router, topo
}

func startTestService(t *testing.T) (*Service, *mockRouter, *mockTopology) {
	t.Helper()
	svc, router, topo := newTestService()
	err := svc.Start(context.Background())
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

func TestNewServiceNilLogger(t *testing.T) {
	svc := NewService(nil, newMockRouter(), newMockTopology(), nil, nil, "node")
	require.NotNil(t, svc)
}

func TestServiceStartStop(t *testing.T) {
	svc, _, _ := newTestService()

	err := svc.Start(context.Background())
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

	err := svc.Broadcast(sender, "workers", "hello", nil)
	require.NoError(t, err)

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

	err := svc.BroadcastLocal(sender, "workers", "hello", nil)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	assert.Len(t, sends, 1)
}

func TestServiceBroadcastEmptyGroup(t *testing.T) {
	svc, router, _ := startTestService(t)

	sender := mkPID("host1", "sender")

	router.reset()

	err := svc.Broadcast(sender, "empty", "hello", nil)
	require.NoError(t, err)

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
	source := pgPID("node-b")
	target := pgPID("local-node")
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
	source := pgPID("node-b")
	target := pgPID("local-node")
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
	source := pgPID("node-b")
	target := pgPID("local-node")
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
	joinPkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), pgapi.TopicJoin,
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
	leavePkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), pgapi.TopicLeave,
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
	pkg := relay.NewMessagePackage(p1, pgPID("local-node"), msg)

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
	pkg := relay.NewMessagePackage(pgPID("node-b"), pgPID("local-node"), msg)

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

	pkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), pgapi.TopicDiscover,
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

	pkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), pgapi.TopicDiscover,
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
	pkg := relay.NewMessagePackage(pgPID("node-b"), pgPID("local-node"), msg)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	// Should not crash
}

func TestServiceSend_SyncWrongPayloadType(t *testing.T) {
	svc, _, _ := startTestService(t)

	pkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), pgapi.TopicSync,
		payload.New(42), // not a map
	)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	// Should not crash
}

func TestServiceSend_SyncEmptyFrom(t *testing.T) {
	svc, _, _ := startTestService(t)

	pkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), pgapi.TopicSync,
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
	pkg := relay.NewMessagePackage(pgPID("node-b"), pgPID("local-node"), msg)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
}

func TestServiceSend_JoinMissingGroup(t *testing.T) {
	svc, _, _ := startTestService(t)

	pkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), pgapi.TopicJoin,
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

	pkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), pgapi.TopicJoin,
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

	pkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), pgapi.TopicJoin,
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
	pkg := relay.NewMessagePackage(pgPID("node-b"), pgPID("local-node"), msg)

	err := svc.Send(pkg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
}

func TestServiceSend_LeaveMissingFrom(t *testing.T) {
	svc, _, _ := startTestService(t)

	pkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), pgapi.TopicLeave,
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

	pkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), pgapi.TopicSync,
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

	pkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), pgapi.TopicSync,
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
	pkg := relay.NewMessagePackage(p1, pgPID("local-node"), msg)

	require.NoError(t, svc.Send(pkg))
	time.Sleep(50 * time.Millisecond)

	// Process should still be a member (exit not processed)
	assert.Len(t, svc.GetMembers("workers"), 1)
}

func TestServiceSend_UnknownTopic(t *testing.T) {
	svc, _, _ := startTestService(t)

	pkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), "unknown.topic",
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
	pkg.Source = pgPID("node-b")
	pkg.Target = pgPID("local-node")

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

	svc := NewService(logger, router, topo, membership, nil, "local-node")

	err := svc.Start(context.Background())
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

	svc := NewService(logger, router, topo, membership, nil, "local-node")

	err := svc.Start(context.Background())
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
		svc.publishSnapshot()
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
	require.NoError(t, svc.Start(context.Background()))
	require.NoError(t, svc.Stop(context.Background()))

	p1 := mkPID("host1", "1")
	err := svc.Join("workers", p1)
	assert.ErrorIs(t, err, ErrServiceStopped)
}

func TestServiceLeaveAfterStop(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, svc.Start(context.Background()))
	require.NoError(t, svc.Stop(context.Background()))

	p1 := mkPID("host1", "1")
	err := svc.Leave("workers", p1)
	assert.ErrorIs(t, err, ErrServiceStopped)
}

func TestServiceGetMembersAfterStop(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, svc.Start(context.Background()))
	require.NoError(t, svc.Stop(context.Background()))

	members := svc.GetMembers("workers")
	assert.Nil(t, members)
}

func TestServiceGetLocalMembersAfterStop(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, svc.Start(context.Background()))
	require.NoError(t, svc.Stop(context.Background()))

	members := svc.GetLocalMembers("workers")
	assert.Nil(t, members)
}

func TestServiceWhichGroupsAfterStop(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, svc.Start(context.Background()))
	require.NoError(t, svc.Stop(context.Background()))

	groups := svc.WhichGroups()
	assert.Nil(t, groups)
}

func TestServiceBroadcastAfterStop(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, svc.Start(context.Background()))
	require.NoError(t, svc.Stop(context.Background()))

	sender := mkPID("host1", "sender")
	err := svc.Broadcast(sender, "workers", "hello", nil)
	assert.ErrorIs(t, err, ErrServiceStopped)
}

func TestServiceBroadcastLocalAfterStop(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, svc.Start(context.Background()))
	require.NoError(t, svc.Stop(context.Background()))

	sender := mkPID("host1", "sender")
	err := svc.BroadcastLocal(sender, "workers", "hello", nil)
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
	err := svc.Broadcast(sender, "workers", "hello", nil)
	require.NoError(t, err) // Broadcast itself doesn't fail

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
	err := svc.Broadcast(sender, "nonexistent", "hello", nil)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	sends := router.getSends()
	assert.Empty(t, sends)
}

// --- Event emission tests ---

// newTestServiceWithBus creates a test service with a real event bus.
func newTestServiceWithBus(t *testing.T) (*Service, *mockRouter, *mockTopology, event.Bus) {
	t.Helper()
	router := newMockRouter()
	topo := newMockTopology()
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	svc := NewService(logger, router, topo, nil, bus, "local-node")
	return svc, router, topo, bus
}

func startTestServiceWithBus(t *testing.T) (*Service, *mockRouter, *mockTopology, event.Bus) {
	t.Helper()
	svc, router, topo, bus := newTestServiceWithBus(t)
	err := svc.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = svc.Stop(context.Background())
	})
	time.Sleep(10 * time.Millisecond)
	return svc, router, topo, bus
}

func TestServiceEmitsJoinEvent(t *testing.T) {
	svc, _, _, bus := startTestServiceWithBus(t)

	// Subscribe to pg events on the event bus
	ch := make(chan event.Event, 8)
	_, err := bus.SubscribeP(context.Background(), pgapi.EventSystem, pgapi.MemberJoined, ch)
	require.NoError(t, err)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	// Wait for event
	select {
	case evt := <-ch:
		assert.Equal(t, event.System(pgapi.EventSystem), evt.System)
		assert.Equal(t, event.Kind(pgapi.MemberJoined), evt.Kind)
		assert.Equal(t, "workers", evt.Path)
		memberEvt, ok := evt.Data.(pgapi.MembershipEvent)
		require.True(t, ok, "expected MembershipEvent, got %T", evt.Data)
		assert.Equal(t, "workers", memberEvt.Group)
		require.Len(t, memberEvt.PIDs, 1)
		assert.Equal(t, p1.String(), memberEvt.PIDs[0].String())
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for join event")
	}
}

func TestServiceEmitsLeaveEvent(t *testing.T) {
	svc, _, _, bus := startTestServiceWithBus(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	// Subscribe to leave events
	ch := make(chan event.Event, 8)
	_, err := bus.SubscribeP(context.Background(), pgapi.EventSystem, pgapi.MemberLeft, ch)
	require.NoError(t, err)

	require.NoError(t, svc.Leave("workers", p1))

	select {
	case evt := <-ch:
		assert.Equal(t, event.System(pgapi.EventSystem), evt.System)
		assert.Equal(t, event.Kind(pgapi.MemberLeft), evt.Kind)
		assert.Equal(t, "workers", evt.Path)
		memberEvt, ok := evt.Data.(pgapi.MembershipEvent)
		require.True(t, ok, "expected MembershipEvent, got %T", evt.Data)
		assert.Equal(t, "workers", memberEvt.Group)
		require.Len(t, memberEvt.PIDs, 1)
		assert.Equal(t, p1.String(), memberEvt.PIDs[0].String())
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for leave event")
	}
}

func TestServiceEmitsEventOnProcessExit(t *testing.T) {
	svc, _, _, bus := startTestServiceWithBus(t)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))

	// Subscribe to leave events
	ch := make(chan event.Event, 8)
	_, err := bus.SubscribeP(context.Background(), pgapi.EventSystem, pgapi.MemberLeft, ch)
	require.NoError(t, err)

	// Simulate process exit via relay
	exitEvent := &topology.ExitEvent{From: p1}
	msg := relay.AcquireMessage()
	msg.Topic = topology.TopicEvents
	msg.Payloads = payload.Payloads{payload.New(exitEvent)}
	pkg := relay.NewMessagePackage(p1, pgPID("local-node"), msg)
	require.NoError(t, svc.Send(pkg))

	select {
	case evt := <-ch:
		assert.Equal(t, event.Kind(pgapi.MemberLeft), evt.Kind)
		memberEvt, ok := evt.Data.(pgapi.MembershipEvent)
		require.True(t, ok)
		assert.Equal(t, "workers", memberEvt.Group)
		require.Len(t, memberEvt.PIDs, 1)
		assert.Equal(t, p1.String(), memberEvt.PIDs[0].String())
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for exit leave event")
	}
}

func TestServiceMultiJoinExitEmitsDedupedEvents(t *testing.T) {
	svc, _, _, bus := startTestServiceWithBus(t)

	p1 := mkPID("host1", "1")
	// Join "workers" 3 times and "managers" once
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("managers", p1))

	// Subscribe to leave events
	ch := make(chan event.Event, 16)
	_, err := bus.SubscribeP(context.Background(), pgapi.EventSystem, pgapi.MemberLeft, ch)
	require.NoError(t, err)
	time.Sleep(20 * time.Millisecond)

	// Simulate process exit
	exitEvent := &topology.ExitEvent{From: p1}
	msg := relay.AcquireMessage()
	msg.Topic = topology.TopicEvents
	msg.Payloads = payload.Payloads{payload.New(exitEvent)}
	pkg := relay.NewMessagePackage(p1, pgPID("local-node"), msg)
	require.NoError(t, svc.Send(pkg))

	// Should get exactly 2 leave events (one per unique group, not 4)
	received := make(map[string]int)
	timeout := time.After(2 * time.Second)
	for len(received) < 2 {
		select {
		case evt := <-ch:
			memberEvt, ok := evt.Data.(pgapi.MembershipEvent)
			require.True(t, ok)
			received[memberEvt.Group]++
		case <-timeout:
			t.Fatalf("expected 2 leave events (workers, managers), got %d: %v", len(received), received)
		}
	}

	assert.Equal(t, 1, received["workers"], "should get exactly 1 leave event for workers")
	assert.Equal(t, 1, received["managers"], "should get exactly 1 leave event for managers")

	// Verify no extra events
	select {
	case evt := <-ch:
		t.Fatalf("unexpected extra leave event: %+v", evt)
	case <-time.After(200 * time.Millisecond):
		// good — no extra events
	}
}

func TestServiceEmitsNoEventWithNilBus(t *testing.T) {
	// Service with nil bus should not panic on join/leave
	svc, _, _ := startTestService(t) // startTestService uses nil bus

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Leave("workers", p1))
	// Should not panic — emitJoinEvent/emitLeaveEvent guard on nil bus
}

func TestServiceEmitsMultipleJoinEvents(t *testing.T) {
	svc, _, _, bus := startTestServiceWithBus(t)

	ch := make(chan event.Event, 16)
	_, err := bus.SubscribeP(context.Background(), pgapi.EventSystem, "**", ch)
	require.NoError(t, err)

	// Give subscription time to register in the bus event loop
	time.Sleep(20 * time.Millisecond)

	p1 := mkPID("host1", "1")
	p2 := mkPID("host1", "2")
	require.NoError(t, svc.Join("workers", p1))
	require.NoError(t, svc.Join("workers", p2))

	// Collect events
	received := 0
	timeout := time.After(2 * time.Second)
	for received < 2 {
		select {
		case evt := <-ch:
			assert.Equal(t, event.Kind(pgapi.MemberJoined), evt.Kind)
			received++
		case <-timeout:
			t.Fatalf("expected 2 join events, got %d", received)
		}
	}
}

func TestServiceEmitsEventsForRemoteJoin(t *testing.T) {
	svc, _, _, bus := startTestServiceWithBus(t)

	ch := make(chan event.Event, 8)
	_, err := bus.SubscribeP(context.Background(), pgapi.EventSystem, pgapi.MemberJoined, ch)
	require.NoError(t, err)

	// Simulate remote join via relay package
	rp1 := mkNodePID("node-b", "host1", "1")
	pkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), pgapi.TopicJoin,
		payload.New(map[string]any{
			"from":  "node-b",
			"group": "workers",
			"pids":  []any{rp1.String()},
		}),
	)
	require.NoError(t, svc.Send(pkg))

	select {
	case evt := <-ch:
		assert.Equal(t, event.Kind(pgapi.MemberJoined), evt.Kind)
		assert.Equal(t, "workers", evt.Path)
		memberEvt, ok := evt.Data.(pgapi.MembershipEvent)
		require.True(t, ok)
		assert.Equal(t, "workers", memberEvt.Group)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for remote join event")
	}
}

func TestServiceEmitsEventsForRemoteLeave(t *testing.T) {
	svc, _, _, bus := startTestServiceWithBus(t)

	// First join remotely
	rp1 := mkNodePID("node-b", "host1", "1")
	joinPkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), pgapi.TopicJoin,
		payload.New(map[string]any{
			"from":  "node-b",
			"group": "workers",
			"pids":  []any{rp1.String()},
		}),
	)
	require.NoError(t, svc.Send(joinPkg))
	time.Sleep(50 * time.Millisecond)

	// Subscribe to leave events
	ch := make(chan event.Event, 8)
	_, err := bus.SubscribeP(context.Background(), pgapi.EventSystem, pgapi.MemberLeft, ch)
	require.NoError(t, err)

	// Remote leave
	leavePkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), pgapi.TopicLeave,
		payload.New(map[string]any{
			"from":   "node-b",
			"pids":   []any{rp1.String()},
			"groups": []any{"workers"},
		}),
	)
	require.NoError(t, svc.Send(leavePkg))

	select {
	case evt := <-ch:
		assert.Equal(t, event.Kind(pgapi.MemberLeft), evt.Kind)
		assert.Equal(t, "workers", evt.Path)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for remote leave event")
	}
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
	require.NoError(t, svc.Start(context.Background()))
	require.NoError(t, svc.Stop(context.Background()))

	p1 := mkPID("host1", "1")
	err := svc.JoinGroups([]string{"workers"}, p1)
	assert.ErrorIs(t, err, ErrServiceStopped)
}

func TestServiceJoinGroupsEmitsEvents(t *testing.T) {
	svc, _, _, bus := startTestServiceWithBus(t)

	ch := make(chan event.Event, 16)
	_, err := bus.SubscribeP(context.Background(), pgapi.EventSystem, pgapi.MemberJoined, ch)
	require.NoError(t, err)
	time.Sleep(20 * time.Millisecond)

	p1 := mkPID("host1", "1")
	require.NoError(t, svc.JoinGroups([]string{"workers", "managers"}, p1))

	received := 0
	timeout := time.After(2 * time.Second)
	for received < 2 {
		select {
		case <-ch:
			received++
		case <-timeout:
			t.Fatalf("expected 2 join events, got %d", received)
		}
	}
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
	joinPkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), pgapi.TopicJoin,
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
	pkg := relay.NewMessagePackage(p1, pgPID("local-node"), msg)
	require.NoError(t, svc.Send(pkg))

	time.Sleep(100 * time.Millisecond)

	// Group should be empty after exit
	assert.Empty(t, svc.GetMembers("workers"))

	// Verify the broadcast leave sent to remote nodes contains "workers" twice
	sends := router.getSends()
	leaveFound := false
	for _, s := range sends {
		for _, m := range s.Messages {
			if m.Topic == pgapi.TopicLeave {
				leaveFound = true
				data, ok := m.Payloads[0].Data().(map[string]any)
				require.True(t, ok)
				rawGroups, _ := data["groups"].([]string)
				// Count "workers" occurrences
				count := 0
				for _, g := range rawGroups {
					if g == "workers" {
						count++
					}
				}
				assert.Equal(t, 2, count,
					"broadcastLeave should send 'workers' twice for multi-join exit")
			}
		}
	}
	assert.True(t, leaveFound, "should have sent at least one leave broadcast")
}

func TestServiceLeaveGroupsAfterStop(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, svc.Start(context.Background()))
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
	pkg := relay.NewPackage(pgPID("node-b"), pgPID("local-node"), pgapi.TopicJoin,
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
	require.NoError(t, svc.Start(context.Background()))
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
	assert.Len(t, result.members, 2)
	assert.NotNil(t, result.unsubscribe)

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
	result.unsubscribe()
	time.Sleep(50 * time.Millisecond)
}

func TestServiceMonitorEmptyGroup(t *testing.T) {
	svc, _, _ := startTestService(t)

	monitorPID := mkPID("host1", "monitor")
	result := svc.Monitor("nonexistent", monitorPID, "pg.event")

	assert.Nil(t, result.members)
	assert.NotNil(t, result.unsubscribe)
	result.unsubscribe()
}

func TestServiceMonitorUnsubscribe(t *testing.T) {
	svc, router, _ := startTestService(t)

	monitorPID := mkPID("host1", "monitor")
	result := svc.Monitor("workers", monitorPID, "pg.event")

	// Unsubscribe immediately
	result.unsubscribe()
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
	assert.Len(t, result.groups, 2)
	assert.Len(t, result.groups["workers"], 1)
	assert.Len(t, result.groups["managers"], 1)
	assert.NotNil(t, result.unsubscribe)

	result.unsubscribe()
}

func TestServiceEventsEmpty(t *testing.T) {
	svc, _, _ := startTestService(t)

	eventsPID := mkPID("host1", "events")
	result := svc.Events(eventsPID, "pg.event")

	assert.Empty(t, result.groups)
	assert.NotNil(t, result.unsubscribe)
	result.unsubscribe()
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

	result.unsubscribe()
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
