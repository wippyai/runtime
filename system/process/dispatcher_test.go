// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/wippyai/runtime/api/attrs"
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

func (m *mockProcessManager) Cancel(ctx context.Context, from, target pid.PID, reason string) error {
	args := m.Called(ctx, from, target, reason)
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
	reason := "test cancel"
	manager.On("Cancel", mock.Anything, fromPID, targetPID, reason).Return(nil)

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.CancelCmd{
		From:   fromPID,
		Target: targetPID,
		Reason: reason,
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
	assert.NotNil(t, registeredHandlers[process.Exec])
}

func TestDispatcher_HandleExec_MissingHostID(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.ExecCmd{
		Source: registry.NewID("test", "handler"),
		HostID: "", // empty host
	}

	receiver := &mockResultReceiver{}
	err := d.handleExec(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)
	assert.NotNil(t, receiver.err)
	assert.Contains(t, receiver.err.Error(), "host ID required")
}

func TestDispatcher_HandleExec_MissingPIDGenerator(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.ExecCmd{
		Source: registry.NewID("test", "handler"),
		HostID: "lua",
	}

	// Context without PID generator
	receiver := &mockResultReceiver{}
	err := d.handleExec(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)
	assert.NotNil(t, receiver.err)
	assert.Contains(t, receiver.err.Error(), "PID generator")
}

func TestDispatcher_HandleExec_MissingNode(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.ExecCmd{
		Source: registry.NewID("test", "handler"),
		HostID: "lua",
	}

	// Context with PID generator but no node
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	pidGen := uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	receiver := &mockResultReceiver{}
	err := d.handleExec(ctx, cmd, 1, receiver)
	assert.NoError(t, err)
	assert.NotNil(t, receiver.err)
	assert.Contains(t, receiver.err.Error(), "relay node")
}

func TestDispatcher_HandleExec_NoTopology(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}

	d := NewDispatcher(manager, router, nil, nil) // nil topology

	cmd := &process.ExecCmd{
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
	err := d.handleExec(ctx, cmd, 1, receiver)
	assert.NoError(t, err)
	assert.NotNil(t, receiver.err)
	assert.Contains(t, receiver.err.Error(), "topology")
}

func TestDispatcher_HandleExec_RegisterError(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	topo.On("Register", mock.Anything).Return(errors.New("register failed"))

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.ExecCmd{
		Source: registry.NewID("test", "handler"),
		HostID: "lua",
	}

	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	pidGen := uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	node := sysrelay.NewNode("test-node")
	ctx = relay.WithNode(ctx, node)

	receiver := &mockResultReceiver{}
	err := d.handleExec(ctx, cmd, 1, receiver)
	assert.NoError(t, err)
	assert.NotNil(t, receiver.err)
	assert.Contains(t, receiver.err.Error(), "register failed")
}

func TestDispatcher_HandleExec_AttachError(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	topo.On("Register", mock.Anything).Return(nil)
	topo.On("Remove", mock.Anything).Return()

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.ExecCmd{
		Source: registry.NewID("test", "handler"),
		HostID: "lua",
	}

	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	pidGen := uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	// Node without proper host registration will fail attach
	node := sysrelay.NewNode("test-node")
	ctx = relay.WithNode(ctx, node)

	receiver := &mockResultReceiver{}
	err := d.handleExec(ctx, cmd, 1, receiver)
	assert.NoError(t, err)

	// Attach fails because no host registered for control
	assert.NotNil(t, receiver.err)
	assert.Contains(t, receiver.err.Error(), "cannot route")
}

// mockAttachableHost implements relay.AttachableReceiver for testing
type mockAttachableHost struct {
	attachedCh chan *relay.Package
	mu         sync.Mutex
}

func (h *mockAttachableHost) Send(_ *relay.Package) error {
	return nil
}

func (h *mockAttachableHost) Attach(_ pid.PID, ch chan *relay.Package) (context.CancelFunc, error) {
	h.mu.Lock()
	h.attachedCh = ch
	h.mu.Unlock()
	return func() {}, nil
}

func (h *mockAttachableHost) Detach(_ pid.PID) {
	h.mu.Lock()
	h.attachedCh = nil
	h.mu.Unlock()
}

func (h *mockAttachableHost) sendExitEvent(result *runtime.Result) {
	h.mu.Lock()
	ch := h.attachedCh
	h.mu.Unlock()

	if ch != nil {
		exitEvent := &topology.ExitEvent{
			Result: result,
		}
		ch <- &relay.Package{
			Messages: []*relay.Message{{
				Topic:    topology.TopicEvents,
				Payloads: payload.Payloads{payload.New(exitEvent)},
			}},
		}
	}
}

func TestDispatcher_HandleExec_Success(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	processPID := pid.PID{Host: "lua", UniqID: "proc-1"}

	topo.On("Register", mock.Anything).Return(nil)
	topo.On("Remove", mock.Anything).Return()
	manager.On("Start", mock.Anything, mock.Anything).Return(processPID, nil)

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.ExecCmd{
		Source: registry.NewID("test", "handler"),
		HostID: "lua",
	}

	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	pidGen := uniqid.NewPIDGenerator(uniqid.NewGenerator(), "")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	// Create node with attachable host for control
	node := sysrelay.NewNode("")
	controlHost := &mockAttachableHost{}
	_ = node.RegisterHost(topology.ControlHost, controlHost)
	ctx = relay.WithNode(ctx, node)

	receiver := &asyncResultReceiver{done: make(chan struct{})}
	err := d.handleExec(ctx, cmd, 1, receiver)
	assert.NoError(t, err)

	// Send exit event through relay
	time.Sleep(10 * time.Millisecond) // Let goroutine start
	controlHost.sendExitEvent(&runtime.Result{Value: payload.New("success")})

	select {
	case <-receiver.done:
		// Got result
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}

	assert.Nil(t, receiver.err)
	result, ok := receiver.data.(process.ExecResult)
	assert.True(t, ok)
	assert.NotNil(t, result.Result)
}

func TestDispatcher_HandleExec_ContextCanceled(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	processPID := pid.PID{Host: "lua", UniqID: "proc-1"}

	topo.On("Register", mock.Anything).Return(nil)
	topo.On("Remove", mock.Anything).Return()
	manager.On("Start", mock.Anything, mock.Anything).Return(processPID, nil)

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.ExecCmd{
		Source: registry.NewID("test", "handler"),
		HostID: "lua",
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
	pidGen := uniqid.NewPIDGenerator(uniqid.NewGenerator(), "")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	node := sysrelay.NewNode("")
	controlHost := &mockAttachableHost{}
	_ = node.RegisterHost(topology.ControlHost, controlHost)
	ctx = relay.WithNode(ctx, node)

	receiver := &asyncResultReceiver{done: make(chan struct{})}
	err := d.handleExec(ctx, cmd, 1, receiver)
	assert.NoError(t, err)

	// Cancel context
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-receiver.done:
		// Got result
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for cancellation")
	}

	assert.NotNil(t, receiver.err)
	assert.ErrorIs(t, receiver.err, context.Canceled)
}

func TestDispatcher_HandleExec_StartError(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	topo.On("Register", mock.Anything).Return(nil)
	topo.On("Remove", mock.Anything).Return()
	manager.On("Start", mock.Anything, mock.Anything).Return(pid.PID{}, errors.New("start failed"))

	d := NewDispatcher(manager, router, topo, nil)

	cmd := &process.ExecCmd{
		Source: registry.NewID("test", "handler"),
		HostID: "lua",
	}

	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	pidGen := uniqid.NewPIDGenerator(uniqid.NewGenerator(), "")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	node := sysrelay.NewNode("")
	controlHost := &mockAttachableHost{}
	_ = node.RegisterHost(topology.ControlHost, controlHost)
	ctx = relay.WithNode(ctx, node)

	receiver := &mockResultReceiver{}
	err := d.handleExec(ctx, cmd, 1, receiver)
	assert.NoError(t, err)

	assert.NotNil(t, receiver.err)
	assert.Contains(t, receiver.err.Error(), "start failed")
}

type asyncResultReceiver struct {
	data any
	err  error
	done chan struct{}
}

func (r *asyncResultReceiver) CompleteYield(_ uint64, data any, err error) {
	r.data = data
	r.err = err
	close(r.done)
}

func TestDispatcher_HandleSpawn_WithMonitor(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	parentPID := pid.PID{Host: "test", UniqID: "parent"}
	spawnedPID := pid.PID{Host: "test", UniqID: "spawned"}
	manager.On("Start", mock.Anything, mock.Anything).Return(spawnedPID, nil)

	d := NewDispatcher(manager, router, topo, nil)

	options := attrs.NewBag()
	options.Set(process.ProcessParentKey, parentPID)
	options.Set(process.ProcessMonitorKey, true)

	cmd := &process.SpawnCmd{
		Start: &process.Start{
			HostID:  "test",
			Options: options,
		},
	}

	receiver := &mockResultReceiver{}
	err := d.handleSpawn(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)

	result, ok := receiver.data.(process.SpawnResult)
	assert.True(t, ok)
	assert.Equal(t, spawnedPID, result.PID)

	manager.AssertExpectations(t)
	topo.AssertNotCalled(t, "Monitor", mock.Anything, mock.Anything)
}

func TestDispatcher_HandleSpawn_WithLink(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	parentPID := pid.PID{Host: "test", UniqID: "parent"}
	spawnedPID := pid.PID{Host: "test", UniqID: "spawned"}
	manager.On("Start", mock.Anything, mock.Anything).Return(spawnedPID, nil)

	d := NewDispatcher(manager, router, topo, nil)

	options := attrs.NewBag()
	options.Set(process.ProcessParentKey, parentPID)
	options.Set(process.ProcessLinkKey, true)

	cmd := &process.SpawnCmd{
		Start: &process.Start{
			HostID:  "test",
			Options: options,
		},
	}

	receiver := &mockResultReceiver{}
	err := d.handleSpawn(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)

	result, ok := receiver.data.(process.SpawnResult)
	assert.True(t, ok)
	assert.Equal(t, spawnedPID, result.PID)

	manager.AssertExpectations(t)
	topo.AssertNotCalled(t, "Link", mock.Anything, mock.Anything)
}

func TestDispatcher_HandleSpawn_MonitorError(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	parentPID := pid.PID{Host: "test", UniqID: "parent"}
	spawnedPID := pid.PID{Host: "test", UniqID: "spawned"}
	manager.On("Start", mock.Anything, mock.Anything).Return(spawnedPID, nil)

	d := NewDispatcher(manager, router, topo, nil)

	options := attrs.NewBag()
	options.Set(process.ProcessParentKey, parentPID)
	options.Set(process.ProcessMonitorKey, true)

	cmd := &process.SpawnCmd{
		Start: &process.Start{
			HostID:  "test",
			Options: options,
		},
	}

	receiver := &mockResultReceiver{}
	err := d.handleSpawn(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)

	// Still returns PID even if monitor fails
	result, ok := receiver.data.(process.SpawnResult)
	assert.True(t, ok)
	assert.Equal(t, spawnedPID, result.PID)

	manager.AssertExpectations(t)
	topo.AssertNotCalled(t, "Monitor", mock.Anything, mock.Anything)
}

func TestDispatcher_HandleSpawn_LinkError(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}
	topo := &mockTopology{}

	parentPID := pid.PID{Host: "test", UniqID: "parent"}
	spawnedPID := pid.PID{Host: "test", UniqID: "spawned"}
	manager.On("Start", mock.Anything, mock.Anything).Return(spawnedPID, nil)

	d := NewDispatcher(manager, router, topo, nil)

	options := attrs.NewBag()
	options.Set(process.ProcessParentKey, parentPID)
	options.Set(process.ProcessLinkKey, true)

	cmd := &process.SpawnCmd{
		Start: &process.Start{
			HostID:  "test",
			Options: options,
		},
	}

	receiver := &mockResultReceiver{}
	err := d.handleSpawn(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)

	// Still returns PID even if link fails
	result, ok := receiver.data.(process.SpawnResult)
	assert.True(t, ok)
	assert.Equal(t, spawnedPID, result.PID)

	manager.AssertExpectations(t)
	topo.AssertNotCalled(t, "Link", mock.Anything, mock.Anything)
}

func TestDispatcher_HandleUnmonitor_NoTopology(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}

	d := NewDispatcher(manager, router, nil, nil)

	cmd := &process.UnmonitorCmd{
		Watcher: pid.PID{Host: "test", UniqID: "watcher"},
		Target:  pid.PID{Host: "test", UniqID: "target"},
	}

	receiver := &mockResultReceiver{}
	err := d.handleUnmonitor(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)
	assert.Nil(t, receiver.err)
}

func TestDispatcher_HandleLink_NoTopology(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}

	d := NewDispatcher(manager, router, nil, nil)

	cmd := &process.LinkCmd{
		From: pid.PID{Host: "test", UniqID: "from"},
		To:   pid.PID{Host: "test", UniqID: "to"},
	}

	receiver := &mockResultReceiver{}
	err := d.handleLink(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)
	assert.Nil(t, receiver.err)
}

func TestDispatcher_HandleUnlink_NoTopology(t *testing.T) {
	manager := &mockProcessManager{}
	router := &mockRouter{}

	d := NewDispatcher(manager, router, nil, nil)

	cmd := &process.UnlinkCmd{
		From: pid.PID{Host: "test", UniqID: "from"},
		To:   pid.PID{Host: "test", UniqID: "to"},
	}

	receiver := &mockResultReceiver{}
	err := d.handleUnlink(context.Background(), cmd, 1, receiver)
	assert.NoError(t, err)
	assert.Nil(t, receiver.err)
}

var _ process.Manager = (*mockProcessManager)(nil)
var _ relay.Receiver = (*mockRouter)(nil)
var _ topology.Topology = (*mockTopology)(nil)
