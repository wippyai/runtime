// SPDX-License-Identifier: MPL-2.0

package internode

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
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// Mock implementations

type mockConnectionManager struct {
	sendError         error
	startError        error
	managedNodes      map[cluster.NodeID]bool
	connectedNodes    map[cluster.NodeID]bool
	onMessage         func(nodeID cluster.NodeID, data []byte)
	ensuredConns      []ensureConnCall
	disconnectedNodes []cluster.NodeID
	mu                sync.Mutex
	started           bool
	stopped           bool
}

type ensureConnCall struct {
	nodeID cluster.NodeID
	addr   string
	port   int
}

func newMockConnectionManager() *mockConnectionManager {
	return &mockConnectionManager{
		managedNodes:   make(map[cluster.NodeID]bool),
		connectedNodes: make(map[cluster.NodeID]bool),
	}
}

func (m *mockConnectionManager) Start(_ context.Context, onMessage func(nodeID cluster.NodeID, data []byte)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startError != nil {
		return m.startError
	}
	m.started = true
	m.onMessage = onMessage
	return nil
}

func (m *mockConnectionManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
	return nil
}

func (m *mockConnectionManager) SendToNode(_ cluster.NodeID, _ []byte, _ Class) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendError != nil {
		return m.sendError
	}
	return nil
}

func (m *mockConnectionManager) EnsureConnection(nodeID cluster.NodeID, addr string, port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensuredConns = append(m.ensuredConns, ensureConnCall{nodeID, addr, port})
	m.connectedNodes[nodeID] = true
}

func (m *mockConnectionManager) DisconnectFromNode(nodeID cluster.NodeID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disconnectedNodes = append(m.disconnectedNodes, nodeID)
	delete(m.connectedNodes, nodeID)
}

func (m *mockConnectionManager) ConnectedNodes() []cluster.NodeID {
	m.mu.Lock()
	defer m.mu.Unlock()
	nodes := make([]cluster.NodeID, 0, len(m.connectedNodes))
	for nodeID := range m.connectedNodes {
		nodes = append(nodes, nodeID)
	}
	return nodes
}

func (m *mockConnectionManager) GetListenPort() int {
	return 9000
}

func (m *mockConnectionManager) AddManagedNode(nodeID cluster.NodeID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.managedNodes[nodeID] = true
}

func (m *mockConnectionManager) RemoveManagedNode(nodeID cluster.NodeID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.managedNodes, nodeID)
}

func (m *mockConnectionManager) IsManaged(nodeID cluster.NodeID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.managedNodes[nodeID]
}

func (m *mockConnectionManager) RecordDropReason(_ string) {}

func (m *mockConnectionManager) EvictOrphanNodes(_ map[cluster.NodeID]struct{}) int { return 0 }

func (m *mockConnectionManager) RegisterClassReceiver(_ Class, _ func(cluster.NodeID, []byte)) bool {
	return true
}

func (m *mockConnectionManager) RegisterClassOverflowHandler(_ Class, _ func(cluster.NodeID)) bool {
	return true
}

type mockCodec struct {
	encodeError error
	decodeError error
	encoded     []byte
}

func (m *mockCodec) Encode(_ *relay.Package) ([]byte, error) {
	if m.encodeError != nil {
		return nil, m.encodeError
	}
	if m.encoded != nil {
		return m.encoded, nil
	}
	return []byte("encoded"), nil
}

func (m *mockCodec) Decode(_ []byte) (*relay.Package, error) {
	if m.decodeError != nil {
		return nil, m.decodeError
	}
	pkg := relay.AcquirePackage()
	pkg.Source = pid.PID{Node: "remote-node", Host: "remote-host", UniqID: "123"}
	return pkg, nil
}

type mockMembership struct {
	localNode cluster.NodeInfo
	nodes     []cluster.NodeInfo
}

func (m *mockMembership) Nodes() []cluster.NodeInfo {
	return m.nodes
}

func (m *mockMembership) LocalNode() cluster.NodeInfo {
	return m.localNode
}

// Tests

func setupService(_ *testing.T) (*Service, *mockConnectionManager, *mockCodec, *eventbus.Bus, context.Context, context.CancelFunc) {
	logger := zap.NewNop()
	connMan := newMockConnectionManager()
	codec := &mockCodec{}
	bus := eventbus.NewBus()

	localNode := cluster.NodeInfo{
		ID:   "local-node",
		Addr: "127.0.0.1",
		Meta: cluster.NodeMeta{"internode_port": "9000"},
	}

	membership := &mockMembership{
		localNode: localNode,
		nodes:     []cluster.NodeInfo{localNode},
	}

	deliveryCallback := func(_ *relay.Package) error {
		return nil
	}

	service := NewService(logger, connMan, codec, deliveryCallback, bus, membership)
	ctx, cancel := context.WithCancel(context.Background())

	return service, connMan, codec, bus, ctx, cancel
}

func TestService_NewService(t *testing.T) {
	logger := zap.NewNop()
	connMan := newMockConnectionManager()
	codec := &mockCodec{}
	bus := eventbus.NewBus()
	membership := &mockMembership{}
	callback := func(_ *relay.Package) error { return nil }

	service := NewService(logger, connMan, codec, callback, bus, membership)

	assert.NotNil(t, service)
	assert.NotNil(t, service.codec)
	assert.NotNil(t, service.connMan)
	assert.NotNil(t, service.bus)
	assert.NotNil(t, service.membership)
}

func TestService_Start_Success(t *testing.T) {
	service, connMan, _, _, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)

	assert.True(t, connMan.started)
	assert.NotNil(t, service.subscriber)

	_ = service.Stop()
}

func TestService_Start_WithPreExistingNodes(t *testing.T) {
	logger := zap.NewNop()
	connMan := newMockConnectionManager()
	codec := &mockCodec{}
	bus := eventbus.NewBus()

	localNode := cluster.NodeInfo{
		ID:   "local-node",
		Addr: "127.0.0.1",
		Meta: cluster.NodeMeta{"internode_port": "9000"},
	}

	remoteNode := cluster.NodeInfo{
		ID:   "remote-node",
		Addr: "192.168.1.100",
		Meta: cluster.NodeMeta{"internode_port": "9001"},
	}

	membership := &mockMembership{
		localNode: localNode,
		nodes:     []cluster.NodeInfo{localNode, remoteNode},
	}

	service := NewService(logger, connMan, codec, func(_ *relay.Package) error { return nil }, bus, membership)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)

	connMan.mu.Lock()
	assert.True(t, connMan.managedNodes["remote-node"])
	assert.Len(t, connMan.ensuredConns, 1)
	assert.Equal(t, "remote-node", connMan.ensuredConns[0].nodeID)
	assert.Equal(t, "192.168.1.100", connMan.ensuredConns[0].addr)
	assert.Equal(t, 9001, connMan.ensuredConns[0].port)
	connMan.mu.Unlock()

	_ = service.Stop()
}

func TestService_Start_ConnectionManagerError(t *testing.T) {
	service, connMan, _, _, ctx, cancel := setupService(t)
	defer cancel()

	connMan.startError = errors.New("connection manager failed")

	err := service.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start connection manager")
}

func TestService_Stop(t *testing.T) {
	service, connMan, _, _, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)

	err = service.Stop()
	assert.NoError(t, err)
	assert.True(t, connMan.stopped)
}

func TestService_Send_Success(t *testing.T) {
	service, _, codec, _, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	pkg := relay.AcquirePackage()
	pkg.Target = pid.PID{Node: "remote-node", Host: "remote-host", UniqID: "123"}
	pkg.Messages = []*relay.Message{
		{Topic: "test.topic"},
	}

	codec.encoded = []byte("test-encoded-data")

	err = service.Send(pkg)
	assert.NoError(t, err)
}

func TestService_Send_EncodeError(t *testing.T) {
	service, _, codec, _, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	codec.encodeError = errors.New("encoding failed")

	pkg := relay.AcquirePackage()
	pkg.Target = pid.PID{Node: "remote-node"}

	err = service.Send(pkg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to encode package")
}

func TestService_Send_SendError(t *testing.T) {
	service, connMan, _, _, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	connMan.sendError = errors.New("send failed")

	pkg := relay.AcquirePackage()
	pkg.Target = pid.PID{Node: "remote-node"}

	err = service.Send(pkg)
	assert.Error(t, err)
}

func TestService_HandleMembershipEvent_NodeJoined(t *testing.T) {
	service, connMan, _, bus, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	newNode := cluster.NodeInfo{
		ID:   "new-node",
		Addr: "192.168.1.200",
		Meta: cluster.NodeMeta{"internode_port": "9002"},
	}

	nodeEvent := cluster.NodeEvent{Node: newNode}

	bus.Send(ctx, event.Event{
		System: cluster.System,
		Kind:   cluster.NodeJoined,
		Path:   "node.joined",
		Data:   nodeEvent,
	})

	time.Sleep(50 * time.Millisecond)

	connMan.mu.Lock()
	assert.True(t, connMan.managedNodes["new-node"])
	assert.Contains(t, connMan.ensuredConns, ensureConnCall{
		nodeID: "new-node",
		addr:   "192.168.1.200",
		port:   9002,
	})
	connMan.mu.Unlock()
}

func TestService_HandleMembershipEvent_NodeLeft(t *testing.T) {
	service, connMan, _, bus, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	connMan.AddManagedNode("departing-node")

	departingNode := cluster.NodeInfo{
		ID:   "departing-node",
		Addr: "192.168.1.201",
		Meta: cluster.NodeMeta{"internode_port": "9003"},
	}

	nodeEvent := cluster.NodeEvent{Node: departingNode}

	bus.Send(ctx, event.Event{
		System: cluster.System,
		Kind:   cluster.NodeLeft,
		Path:   "node.left",
		Data:   nodeEvent,
	})

	time.Sleep(50 * time.Millisecond)

	connMan.mu.Lock()
	assert.False(t, connMan.managedNodes["departing-node"])
	connMan.mu.Unlock()
}

func TestService_HandleMembershipEvent_LocalNode(t *testing.T) {
	service, connMan, _, bus, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	localNode := service.membership.LocalNode()
	nodeEvent := cluster.NodeEvent{Node: localNode}

	initialManaged := len(connMan.managedNodes)

	bus.Send(ctx, event.Event{
		System: cluster.System,
		Kind:   cluster.NodeJoined,
		Path:   "node.joined",
		Data:   nodeEvent,
	})

	time.Sleep(50 * time.Millisecond)

	connMan.mu.Lock()
	assert.Len(t, connMan.managedNodes, initialManaged)
	connMan.mu.Unlock()
}

func TestService_HandleMembershipEvent_InvalidData(t *testing.T) {
	service, _, _, bus, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	bus.Send(ctx, event.Event{
		System: cluster.System,
		Kind:   cluster.NodeJoined,
		Path:   "node.joined",
		Data:   "invalid-data",
	})

	time.Sleep(50 * time.Millisecond)
}

func TestService_ConnectToNode_MissingPort(t *testing.T) {
	service, connMan, _, _, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	nodeInfo := cluster.NodeInfo{
		ID:   "no-port-node",
		Addr: "192.168.1.202",
		Meta: cluster.NodeMeta{},
	}

	service.connectToNode(nodeInfo)

	connMan.mu.Lock()
	assert.Len(t, connMan.ensuredConns, 0)
	connMan.mu.Unlock()
}

func TestService_ConnectToNode_InvalidPort(t *testing.T) {
	service, connMan, _, _, ctx, cancel := setupService(t)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	nodeInfo := cluster.NodeInfo{
		ID:   "bad-port-node",
		Addr: "192.168.1.203",
		Meta: cluster.NodeMeta{"internode_port": "not-a-number"},
	}

	service.connectToNode(nodeInfo)

	connMan.mu.Lock()
	assert.Len(t, connMan.ensuredConns, 0)
	connMan.mu.Unlock()
}

func TestService_OnMessage_Success(t *testing.T) {
	deliveryCalled := false
	var deliveredPkg *relay.Package

	logger := zap.NewNop()
	connMan := newMockConnectionManager()
	codec := &mockCodec{}
	bus := eventbus.NewBus()
	membership := &mockMembership{localNode: cluster.NodeInfo{ID: "local"}}

	deliveryCallback := func(pkg *relay.Package) error {
		deliveryCalled = true
		deliveredPkg = pkg
		return nil
	}

	service := NewService(logger, connMan, codec, deliveryCallback, bus, membership)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	connMan.onMessage("remote-node", []byte("incoming-data"))

	assert.True(t, deliveryCalled)
	assert.NotNil(t, deliveredPkg)
}

func TestService_OnMessage_DecodeError(t *testing.T) {
	logger := zap.NewNop()
	connMan := newMockConnectionManager()
	codec := &mockCodec{decodeError: errors.New("decode failed")}
	bus := eventbus.NewBus()
	membership := &mockMembership{localNode: cluster.NodeInfo{ID: "local"}}

	service := NewService(logger, connMan, codec, func(_ *relay.Package) error { return nil }, bus, membership)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	connMan.onMessage("remote-node", []byte("bad-data"))
}

func TestService_OnMessage_DeliveryError(t *testing.T) {
	logger := zap.NewNop()
	connMan := newMockConnectionManager()
	codec := &mockCodec{}
	bus := eventbus.NewBus()
	membership := &mockMembership{localNode: cluster.NodeInfo{ID: "local"}}

	deliveryCallback := func(_ *relay.Package) error {
		return errors.New("delivery failed")
	}

	service := NewService(logger, connMan, codec, deliveryCallback, bus, membership)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	connMan.onMessage("remote-node", []byte("data"))
}

func TestClassForTopic(t *testing.T) {
	cases := []struct {
		topic string
		want  Class
	}{
		{"pg.join", ClassRaftControl},
		{"pg.leave", ClassRaftControl},
		{"pg.discover", ClassRaftControl},
		{"pg.sync", ClassRaftControl},
		{"app.broadcast.ping", ClassPGBroadcast},
		{"", ClassPGBroadcast}, // unknown defaults to broadcast
	}
	for _, c := range cases {
		if got := ClassForTopic(c.topic); got != c.want {
			t.Errorf("ClassForTopic(%q) = %v, want %v", c.topic, got, c.want)
		}
	}
}
