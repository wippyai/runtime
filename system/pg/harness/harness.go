// SPDX-License-Identifier: MPL-2.0

// Package harness provides a test harness for end-to-end PG (process groups) testing.
package harness

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/pg"
	"go.uber.org/zap"
)

// TestNode represents a single node in the test cluster.
type TestNode struct {
	ID       string
	Service  *pg.Service
	Topology *mockTopology
	Router   *mockRouter
	Bus      event.Bus
	Logger   *zap.Logger
}

// TestCluster represents a multi-node PG test cluster.
type TestCluster struct {
	Nodes  map[string]*TestNode
	Logger *zap.Logger
	mu     sync.RWMutex
	context.Context
	cancel context.CancelFunc
}

// NewTestCluster creates a new test cluster with the specified number of nodes.
func NewTestCluster(t testing.TB, nodeCount int) *TestCluster {
	logger := zap.NewNop()
	if testing.Verbose() {
		var err error
		logger, err = zap.NewDevelopment()
		require.NoError(t, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cluster := &TestCluster{
		Nodes:   make(map[string]*TestNode),
		Logger:  logger,
		Context: ctx,
		cancel:  cancel,
	}

	for i := 0; i < nodeCount; i++ {
		nodeID := fmt.Sprintf("node-%d", i)
		node := cluster.createNode(nodeID)
		cluster.Nodes[nodeID] = node
	}

	return cluster
}

// createNode creates a single test node.
func (tc *TestCluster) createNode(nodeID string) *TestNode {
	bus := eventbus.NewBus()
	topo := newMockTopology()
	router := newMockRouter()

	svc := pg.NewService(
		tc.Logger,
		"pg",
		nil,
		router,
		topo,
		nil,
		bus,
		nodeID,
	)

	return &TestNode{
		ID:       nodeID,
		Service:  svc,
		Topology: topo,
		Router:   router,
		Bus:      bus,
		Logger:   tc.Logger,
	}
}

// Start starts all nodes in the cluster.
func (tc *TestCluster) Start(t testing.TB) {
	for _, node := range tc.Nodes {
		_, err := node.Service.Start(tc)
		require.NoError(t, err)
	}
}

// Stop stops all nodes in the cluster.
func (tc *TestCluster) Stop(t testing.TB) {
	tc.cancel()
	for _, node := range tc.Nodes {
		err := node.Service.Stop(context.Background())
		require.NoError(t, err)
	}
}

// GetNode returns a node by ID.
func (tc *TestCluster) GetNode(id string) (*TestNode, bool) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	node, ok := tc.Nodes[id]
	return node, ok
}

// JoinGroup joins a process to a group on a specific node.
func (tc *TestCluster) JoinGroup(t testing.TB, nodeID string, group pgapi.Group, p pid.PID) {
	node, ok := tc.GetNode(nodeID)
	require.True(t, ok, "node %s not found", nodeID)

	err := node.Service.Join(group, p)
	require.NoError(t, err, "failed to join group on %s", nodeID)
}

// LeaveGroup removes a process from a group on a specific node.
func (tc *TestCluster) LeaveGroup(t testing.TB, nodeID string, group pgapi.Group, p pid.PID) {
	node, ok := tc.GetNode(nodeID)
	require.True(t, ok, "node %s not found", nodeID)

	err := node.Service.Leave(group, p)
	require.NoError(t, err, "failed to leave group on %s", nodeID)
}

// GetMembers returns all members of a group from a specific node's view.
func (tc *TestCluster) GetMembers(t testing.TB, nodeID string, group pgapi.Group) []pid.PID {
	node, ok := tc.GetNode(nodeID)
	require.True(t, ok, "node %s not found", nodeID)

	return node.Service.GetMembers(group)
}

// AssertGroupMembers asserts that a group has the expected members on all nodes.
func (tc *TestCluster) AssertGroupMembers(t testing.TB, group pgapi.Group, expected []pid.PID) {
	for nodeID, node := range tc.Nodes {
		members := node.Service.GetMembers(group)
		assert.ElementsMatch(t, expected, members,
			"group %s members mismatch on node %s", group, nodeID)
	}
}

// AssertGroupSize asserts that a group has the expected size on all nodes.
func (tc *TestCluster) AssertGroupSize(t testing.TB, group pgapi.Group, expected int) {
	for nodeID, node := range tc.Nodes {
		members := node.Service.GetMembers(group)
		assert.Len(t, members, expected,
			"group %s size mismatch on node %s", group, nodeID)
	}
}

// WaitForSync waits for all nodes to have consistent view of a group.
func (tc *TestCluster) WaitForSync(t testing.TB, group pgapi.Group, timeout time.Duration) {
	deadline := time.After(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for group %s to sync", group)
		case <-ticker.C:
			if tc.isGroupSynced(group) {
				return
			}
		}
	}
}

// isGroupSynced checks if all nodes have the same view of a group.
func (tc *TestCluster) isGroupSynced(group pgapi.Group) bool {
	var reference []pid.PID
	first := true

	for _, node := range tc.Nodes {
		members := node.Service.GetMembers(group)
		if first {
			reference = members
			first = false
		} else if !pidSlicesEqual(reference, members) {
			return false
		}
	}
	return true
}

// Broadcast sends a message to all members of a group.
func (tc *TestCluster) Broadcast(t testing.TB, nodeID string, from pid.PID, group pgapi.Group, topic string, payloads payload.Payloads) int {
	node, ok := tc.GetNode(nodeID)
	require.True(t, ok, "node %s not found", nodeID)

	count, err := node.Service.Broadcast(from, group, topic, payloads)
	require.NoError(t, err, "failed to broadcast from %s", nodeID)
	return count
}

// SimulateNodeFailure simulates a node failure.
func (tc *TestCluster) SimulateNodeFailure(t testing.TB, nodeID string) {
	node, ok := tc.GetNode(nodeID)
	require.True(t, ok, "node %s not found", nodeID)

	err := node.Service.Stop(context.Background())
	require.NoError(t, err)
}

// RecoverNode recovers a failed node.
func (tc *TestCluster) RecoverNode(t testing.TB, nodeID string) {
	node, ok := tc.GetNode(nodeID)
	require.True(t, ok, "node %s not found", nodeID)

	_, err := node.Service.Start(tc)
	require.NoError(t, err)
}

// pidSlicesEqual checks if two PID slices are equal (order-independent).
func pidSlicesEqual(a, b []pid.PID) bool {
	if len(a) != len(b) {
		return false
	}
	aMap := make(map[string]bool)
	for _, p := range a {
		aMap[p.String()] = true
	}
	for _, p := range b {
		if !aMap[p.String()] {
			return false
		}
	}
	return true
}

// mockRouter is a mock relay router for testing.
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

// mockTopology is a mock topology for testing.
type mockTopology struct {
	monitored  map[string]bool
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
func (m *mockTopology) Monitor(_, target pid.PID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.monitorErr != nil {
		return m.monitorErr
	}
	m.monitored[target.String()] = true
	return nil
}
func (m *mockTopology) Demonitor(_, target pid.PID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.monitored, target.String())
	return nil
}
func (m *mockTopology) Link(_, _ pid.PID) error      { return nil }
func (m *mockTopology) Unlink(_, _ pid.PID) error    { return nil }
func (m *mockTopology) GetLinks(_ pid.PID) []pid.PID { return nil }

var _ topology.Topology = (*mockTopology)(nil)

// MakeTestPID creates a test PID.
func MakeTestPID(node, id string) pid.PID {
	return pid.PID{
		Node:   node,
		Host:   "test",
		UniqID: id,
	}
}

// MakeTestPIDWithHost creates a test PID with specific host.
func MakeTestPIDWithHost(node, host, id string) pid.PID {
	return pid.PID{
		Node:   node,
		Host:   host,
		UniqID: id,
	}
}

// WaitForCondition waits for a condition to be true.
func WaitForCondition(t testing.TB, condition func() bool, timeout time.Duration, msg string) {
	deadline := time.After(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.Fatalf("timeout: %s", msg)
		case <-ticker.C:
			if condition() {
				return
			}
		}
	}
}

// EventCollector collects events.
type EventCollector struct {
	mu     sync.Mutex
	events []event.Event
}

// NewEventCollector creates a new event collector.
func NewEventCollector() *EventCollector {
	return &EventCollector{
		events: make([]event.Event, 0),
	}
}

// Collect starts collecting events.
func (ec *EventCollector) Collect(ctx context.Context, bus event.Bus, system, kind string) {
	ch := make(chan event.Event, 100)
	_, _ = bus.SubscribeP(ctx, system, kind, ch)

	go func() {
		for e := range ch {
			ec.mu.Lock()
			ec.events = append(ec.events, e)
			ec.mu.Unlock()
		}
	}()
}

// GetEvents returns collected events.
func (ec *EventCollector) GetEvents() []event.Event {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	result := make([]event.Event, len(ec.events))
	copy(result, ec.events)
	return result
}
