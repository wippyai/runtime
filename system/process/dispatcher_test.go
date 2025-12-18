package process

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/internal/uniqid"
	sysrelay "github.com/wippyai/runtime/system/relay"
)

type mockProcessManager struct {
	mock.Mock
}

func (m *mockProcessManager) Start(ctx context.Context, start *process.Start) (pid.PID, error) {
	args := m.Called(ctx, start)
	return args.Get(0).(pid.PID), args.Error(1)
}

func (m *mockProcessManager) Terminate(ctx context.Context, target pid.PID) error {
	args := m.Called(ctx, target)
	return args.Error(0)
}

func (m *mockProcessManager) Cancel(ctx context.Context, from, target pid.PID, deadline time.Time) error {
	args := m.Called(ctx, from, target, deadline)
	return args.Error(0)
}

type mockRouter struct {
	mock.Mock
}

func (m *mockRouter) Send(pkg *relay.Package) error {
	args := m.Called(pkg)
	return args.Error(0)
}

type mockTopology struct {
	mock.Mock
}

func (m *mockTopology) Register(p pid.PID) error {
	args := m.Called(p)
	return args.Error(0)
}

func (m *mockTopology) Complete(p pid.PID, result *runtime.Result) {
	m.Called(p, result)
}

func (m *mockTopology) Remove(p pid.PID) {
	m.Called(p)
}

func (m *mockTopology) Monitor(caller, target pid.PID) error {
	args := m.Called(caller, target)
	return args.Error(0)
}

func (m *mockTopology) Demonitor(caller, target pid.PID) error {
	args := m.Called(caller, target)
	return args.Error(0)
}

func (m *mockTopology) Link(from, to pid.PID) error {
	args := m.Called(from, to)
	return args.Error(0)
}

func (m *mockTopology) Unlink(from, to pid.PID) error {
	args := m.Called(from, to)
	return args.Error(0)
}

func (m *mockTopology) GetLinks(p pid.PID) []pid.PID {
	args := m.Called(p)
	return args.Get(0).([]pid.PID)
}

type mockResultReceiver struct {
	data any
	err  error
}

func (m *mockResultReceiver) CompleteYield(_ uint64, data any, err error) {
	m.data = data
	m.err = err
}

func TestNewDispatcher(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	d := NewDispatcher(manager, router, topo, nil)
	assert.NotNil(t, d)
	assert.NotNil(t, d.logger)
}

func TestDispatcher_HandleSend(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	router.On("Send", mock.Anything).Return(nil)

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.SendCmd{
		From:     pid.PID{Host: "test", UniqID: "sender"},
		To:       pid.PID{Host: "test", UniqID: "receiver"},
		Topic:    "hello",
		Payloads: payload.Payloads{payload.NewPayload("test", payload.String)},
	}

	receiver := &mockResultReceiver{}
	err := d.handleSend(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)
	assert.Nil(t, receiver.err)

	result, ok := receiver.data.(process.SendResult)
	assert.True(t, ok)
	assert.Nil(t, result.Error)

	router.AssertExpectations(t)
}

func TestDispatcher_HandleSend_Error(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	sendErr := errors.New("send failed")
	router.On("Send", mock.Anything).Return(sendErr)

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.SendCmd{
		From:  pid.PID{Host: "test", UniqID: "sender"},
		To:    pid.PID{Host: "test", UniqID: "receiver"},
		Topic: "hello",
	}

	receiver := &mockResultReceiver{}
	err := d.handleSend(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)

	result, ok := receiver.data.(process.SendResult)
	assert.True(t, ok)
	assert.Equal(t, sendErr, result.Error)

	router.AssertExpectations(t)
}

func TestDispatcher_HandleSpawn(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	spawnedPID := pid.PID{Host: "test", UniqID: "spawned"}
	manager.On("Start", mock.Anything, mock.Anything).Return(spawnedPID, nil)

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.SpawnCmd{
		Start: &process.Start{
			HostID: "test",
		},
	}

	receiver := &mockResultReceiver{}
	err := d.handleSpawn(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)
	assert.Nil(t, receiver.err)

	result, ok := receiver.data.(process.SpawnResult)
	assert.True(t, ok)
	assert.Equal(t, spawnedPID, result.PID)
	assert.Nil(t, result.Error)

	manager.AssertExpectations(t)
}

func TestDispatcher_HandleSpawn_Error(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	spawnErr := errors.New("spawn failed")
	manager.On("Start", mock.Anything, mock.Anything).Return(pid.PID{}, spawnErr)

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.SpawnCmd{
		Start: &process.Start{
			HostID: "test",
		},
	}

	receiver := &mockResultReceiver{}
	err := d.handleSpawn(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)

	result, ok := receiver.data.(process.SpawnResult)
	assert.True(t, ok)
	assert.Equal(t, spawnErr, result.Error)

	manager.AssertExpectations(t)
}

func TestDispatcher_HandleTerminate(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	targetPID := pid.PID{Host: "test", UniqID: "target"}
	manager.On("Terminate", mock.Anything, targetPID).Return(nil)

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.TerminateCmd{
		Target: targetPID,
	}

	receiver := &mockResultReceiver{}
	err := d.handleTerminate(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)
	assert.Nil(t, receiver.err)

	manager.AssertExpectations(t)
}

func TestDispatcher_HandleCancel(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	fromPID := pid.PID{Host: "test", UniqID: "from"}
	targetPID := pid.PID{Host: "test", UniqID: "target"}
	deadline := time.Now().Add(5 * time.Second)
	manager.On("Cancel", mock.Anything, fromPID, targetPID, deadline).Return(nil)

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.CancelCmd{
		From:     fromPID,
		Target:   targetPID,
		Deadline: deadline,
	}

	receiver := &mockResultReceiver{}
	err := d.handleCancel(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)
	assert.Nil(t, receiver.err)

	manager.AssertExpectations(t)
}

func TestDispatcher_HandleMonitor(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	watcherPID := pid.PID{Host: "test", UniqID: "watcher"}
	targetPID := pid.PID{Host: "test", UniqID: "target"}
	topo.On("Monitor", watcherPID, targetPID).Return(nil)

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.MonitorCmd{
		Watcher: watcherPID,
		Target:  targetPID,
	}

	receiver := &mockResultReceiver{}
	err := d.handleMonitor(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)
	assert.Nil(t, receiver.err)

	topo.AssertExpectations(t)
}

func TestDispatcher_HandleMonitor_NoTopology(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}

	d := NewDispatcher(manager, router, nil, nil)

	cmd := &process.MonitorCmd{
		Watcher: pid.PID{Host: "test", UniqID: "watcher"},
		Target:  pid.PID{Host: "test", UniqID: "target"},
	}

	receiver := &mockResultReceiver{}
	err := d.handleMonitor(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)
	assert.Nil(t, receiver.err)
}

func TestDispatcher_HandleUnmonitor(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	watcherPID := pid.PID{Host: "test", UniqID: "watcher"}
	targetPID := pid.PID{Host: "test", UniqID: "target"}
	topo.On("Demonitor", watcherPID, targetPID).Return(nil)

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.UnmonitorCmd{
		Watcher: watcherPID,
		Target:  targetPID,
	}

	receiver := &mockResultReceiver{}
	err := d.handleUnmonitor(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)
	assert.Nil(t, receiver.err)

	topo.AssertExpectations(t)
}

func TestDispatcher_HandleLink(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	fromPID := pid.PID{Host: "test", UniqID: "from"}
	toPID := pid.PID{Host: "test", UniqID: "to"}
	topo.On("Link", fromPID, toPID).Return(nil)

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.LinkCmd{
		From: fromPID,
		To:   toPID,
	}

	receiver := &mockResultReceiver{}
	err := d.handleLink(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)
	assert.Nil(t, receiver.err)

	topo.AssertExpectations(t)
}

func TestDispatcher_HandleUnlink(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	fromPID := pid.PID{Host: "test", UniqID: "from"}
	toPID := pid.PID{Host: "test", UniqID: "to"}
	topo.On("Unlink", fromPID, toPID).Return(nil)

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.UnlinkCmd{
		From: fromPID,
		To:   toPID,
	}

	receiver := &mockResultReceiver{}
	err := d.handleUnlink(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)
	assert.Nil(t, receiver.err)

	topo.AssertExpectations(t)
}

func TestDispatcher_RegisterAll(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	d := NewDispatcher(manager, router, topo, nil)

	registeredHandlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	register := func(id dispatcher.CommandID, h dispatcher.Handler) {
		registeredHandlers[id] = h
	}

	d.RegisterAll(register)

	assert.NotNil(t, registeredHandlers[process.Send])
	assert.NotNil(t, registeredHandlers[process.Spawn])
	assert.NotNil(t, registeredHandlers[process.Terminate])
	assert.NotNil(t, registeredHandlers[process.Cancel])
	assert.NotNil(t, registeredHandlers[process.Monitor])
	assert.NotNil(t, registeredHandlers[process.Unmonitor])
	assert.NotNil(t, registeredHandlers[process.Link])
	assert.NotNil(t, registeredHandlers[process.Unlink])
	assert.NotNil(t, registeredHandlers[process.Call])
}

func TestDispatcher_HandleCall_MissingHostID(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.CallCmd{
		Source: registry.NewID("test", "handler"),
		HostID: "", // empty host
	}

	receiver := &mockResultReceiver{}
	err := d.handleCall(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)
	assert.NotNil(t, receiver.err)
	assert.Contains(t, receiver.err.Error(), "host ID required")
}

func TestDispatcher_HandleCall_MissingPIDGenerator(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.CallCmd{
		Source: registry.NewID("test", "handler"),
		HostID: "lua",
	}

	// Context without PID generator
	receiver := &mockResultReceiver{}
	err := d.handleCall(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)
	assert.NotNil(t, receiver.err)
	assert.Contains(t, receiver.err.Error(), "PID generator")
}

func TestDispatcher_HandleCall_MissingNode(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.CallCmd{
		Source: registry.NewID("test", "handler"),
		HostID: "lua",
	}

	// Context with PID generator but no node
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	pidGen := uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	receiver := &mockResultReceiver{}
	err := d.handleCall(ctx, cmd, 1, receiver)
	assert.NoError(t, err)
	assert.NotNil(t, receiver.err)
	assert.Contains(t, receiver.err.Error(), "relay node")
}

func TestDispatcher_HandleCall_NoTopology(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}

	d := NewDispatcher(manager, router, nil, nil) // nil topology

	cmd := &process.CallCmd{
		Source: registry.NewID("test", "handler"),
		HostID: "lua",
	}

	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	pidGen := uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	// Use real node for testing
	node := sysrelay.NewNode("test-node")
	ctx = relay.WithNode(ctx, node)

	receiver := &mockResultReceiver{}
	err := d.handleCall(ctx, cmd, 1, receiver)
	assert.NoError(t, err)
	assert.NotNil(t, receiver.err)
	assert.Contains(t, receiver.err.Error(), "topology")
}

var _ process.Manager = (*mockProcessManager)(nil)
var _ relay.Receiver = (*mockRouter)(nil)
var _ topology.Topology = (*mockTopology)(nil)
