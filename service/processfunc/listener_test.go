package processfunc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func newTestPIDGen() *uniqid.PIDGenerator {
	return uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test-node")
}

func waitForEvent(t *testing.T, ch <-chan event.Event, timeout time.Duration) event.Event {
	select {
	case e := <-ch:
		return e
	case <-time.After(timeout):
		t.Fatal("timeout waiting for event")
		return event.Event{}
	}
}

func newTestListener(t *testing.T) (*Listener, event.Bus, <-chan event.Event) {
	bus := eventbus.NewBus()
	pidGen := newTestPIDGen()
	log := zap.NewNop()
	node := &mockNode{}
	topo := &mockTopology{}
	mgr := &mockManager{}

	events := make(chan event.Event, 10)
	sub, err := bus.Subscribe(context.Background(), function.System, events)
	require.NoError(t, err)
	t.Cleanup(func() { bus.Unsubscribe(context.Background(), sub) })

	l := NewListener(log, bus, pidGen, node, topo, mgr)
	return l, bus, events
}

func TestListener_Add_WithDefaultHost(t *testing.T) {
	l, _, events := newTestListener(t)

	meta := attrs.NewBag()
	meta.Set("default_host", "test-host")

	entry := registry.Entry{
		ID:   registry.NewID("test", "proc1"),
		Kind: "process.lua",
		Meta: meta,
	}

	err = l.Add(context.Background(), entry)
	require.NoError(t, err)

	evt := waitForEvent(t, events, 100*time.Millisecond)
	assert.Equal(t, function.System, evt.System)
	assert.Equal(t, function.Register, evt.Kind)
	assert.Equal(t, "test:proc1", evt.Path)

	l.mu.RLock()
	_, exists := l.registered["test:proc1"]
	l.mu.RUnlock()
	assert.True(t, exists)
}

func TestListener_Add_WithoutDefaultHost(t *testing.T) {
	l, _, events := newTestListener(t)

	entry := registry.Entry{
		ID:   registry.NewID("test", "proc1"),
		Kind: "process.lua",
		Meta: attrs.NewBag(),
	}

	err = l.Add(context.Background(), entry)
	require.NoError(t, err)

	// No events should be sent
	select {
	case <-events:
		t.Fatal("unexpected event")
	case <-time.After(10 * time.Millisecond):
		// Expected - no event
	}

	l.mu.RLock()
	_, exists := l.registered["test:proc1"]
	l.mu.RUnlock()
	assert.False(t, exists)
}

func TestListener_Add_NonProcessKind(t *testing.T) {
	l, _, events := newTestListener(t)

	entry := registry.Entry{
		ID:   registry.NewID("test", "svc1"),
		Kind: "service.http",
		Meta: attrs.NewBag(),
	}

	err = l.Add(context.Background(), entry)
	require.NoError(t, err)

	// No events should be sent for non-process kinds
	select {
	case <-events:
		t.Fatal("unexpected event")
	case <-time.After(10 * time.Millisecond):
		// Expected - no event
	}
}

func TestListener_Update_HostChange(t *testing.T) {
	l, _, events := newTestListener(t)

	meta := attrs.NewBag()
	meta.Set("default_host", "host1")

	entry := registry.Entry{
		ID:   registry.NewID("test", "proc1"),
		Kind: "process.lua",
		Meta: meta,
	}

	err = l.Add(context.Background(), entry)
	require.NoError(t, err)

	// Consume the Add event
	waitForEvent(t, events, 100*time.Millisecond)

	// Update with different host
	meta2 := attrs.NewBag()
	meta2.Set("default_host", "host2")
	entry.Meta = meta2

	err = l.Update(context.Background(), entry)
	require.NoError(t, err)

	// Should receive Delete then Register
	evt1 := waitForEvent(t, events, 100*time.Millisecond)
	assert.Equal(t, function.Delete, evt1.Kind)

	evt2 := waitForEvent(t, events, 100*time.Millisecond)
	assert.Equal(t, function.Register, evt2.Kind)
}

func TestListener_Delete(t *testing.T) {
	l, _, events := newTestListener(t)

	meta := attrs.NewBag()
	meta.Set("default_host", "test-host")

	entry := registry.Entry{
		ID:   registry.NewID("test", "proc1"),
		Kind: "process.lua",
		Meta: meta,
	}

	err = l.Add(context.Background(), entry)
	require.NoError(t, err)

	// Consume the Add event
	waitForEvent(t, events, 100*time.Millisecond)

	err = l.Delete(context.Background(), entry)
	require.NoError(t, err)

	evt := waitForEvent(t, events, 100*time.Millisecond)
	assert.Equal(t, function.System, evt.System)
	assert.Equal(t, function.Delete, evt.Kind)

	l.mu.RLock()
	_, exists := l.registered["test:proc1"]
	l.mu.RUnlock()
	assert.False(t, exists)
}

func TestListener_OptionsFromMeta(t *testing.T) {
	l, _, events := newTestListener(t)

	options := attrs.NewBag()
	options.Set("default_host", "test-host")
	options.Set("timeout", "30s")

	meta := attrs.NewBag()
	meta.Set("options", options)

	entry := registry.Entry{
		ID:   registry.NewID("test", "proc1"),
		Kind: "process.lua",
		Meta: meta,
	}

	err = l.Add(context.Background(), entry)
	require.NoError(t, err)

	evt := waitForEvent(t, events, 100*time.Millisecond)
	funcEntry, ok := evt.Data.(*function.FuncEntry)
	require.True(t, ok)
	assert.NotNil(t, funcEntry.Options)
	assert.Equal(t, "30s", funcEntry.Options.GetString("timeout", ""))
}

func TestListener_Update_NoChange(t *testing.T) {
	l, _, events := newTestListener(t)

	meta := attrs.NewBag()
	meta.Set("default_host", "test-host")

	entry := registry.Entry{
		ID:   registry.NewID("test", "proc1"),
		Kind: "process.lua",
		Meta: meta,
	}

	err = l.Add(context.Background(), entry)
	require.NoError(t, err)

	// Consume the Add event
	waitForEvent(t, events, 100*time.Millisecond)

	// Update with same host
	err = l.Update(context.Background(), entry)
	require.NoError(t, err)

	// No events should be sent since host didn't change
	select {
	case <-events:
		t.Fatal("unexpected event - host didn't change")
	case <-time.After(10 * time.Millisecond):
		// Expected - no event
	}
}

func TestListener_Delete_NotRegistered(t *testing.T) {
	l, _, events := newTestListener(t)

	entry := registry.Entry{
		ID:   registry.NewID("test", "proc1"),
		Kind: "process.lua",
		Meta: attrs.NewBag(),
	}

	// Delete without prior registration
	err = l.Delete(context.Background(), entry)
	require.NoError(t, err)

	// No events should be sent
	select {
	case <-events:
		t.Fatal("unexpected event")
	case <-time.After(10 * time.Millisecond):
		// Expected - no event
	}
}

func TestListener_ImplementsEntryListener(_ *testing.T) {
	var _ registry.EntryListener = (*Listener)(nil)
}

// processHandler.Call error tests

func TestProcessHandler_Call_NoRelayNode(t *testing.T) {
	handler := &processHandler{
		log:       zap.NewNop(),
		pidGen:    newTestPIDGen(),
		processID: registry.NewID("test", "proc1"),
		hostID:    "test-host",
	}

	ctx := context.Background()
	task := runtime.Task{}

	_, err := handler.Call(ctx, task)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoRelayNode)
}

func TestProcessHandler_Call_NoTopology(t *testing.T) {
	handler := &processHandler{
		log:       zap.NewNop(),
		pidGen:    newTestPIDGen(),
		processID: registry.NewID("test", "proc1"),
		hostID:    "test-host",
	}

	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	ctx = relayapi.WithNode(ctx, &mockNode{})
	task := runtime.Task{}

	_, err := handler.Call(ctx, task)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoTopology)
}

func TestProcessHandler_Call_NoProcessManager(t *testing.T) {
	handler := &processHandler{
		log:       zap.NewNop(),
		pidGen:    newTestPIDGen(),
		processID: registry.NewID("test", "proc1"),
		hostID:    "test-host",
	}

	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	ctx = relayapi.WithNode(ctx, &mockNode{})
	ctx = topology.WithTopology(ctx, &mockTopology{})
	task := runtime.Task{}

	_, err := handler.Call(ctx, task)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoProcessManager)
}

func TestProcessHandler_Call_RegisterPIDError(t *testing.T) {
	handler := &processHandler{
		log:       zap.NewNop(),
		pidGen:    newTestPIDGen(),
		processID: registry.NewID("test", "proc1"),
		hostID:    "test-host",
	}

	topo := &mockTopology{registerErr: assert.AnError}

	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	ctx = relayapi.WithNode(ctx, &mockNode{})
	ctx = topology.WithTopology(ctx, topo)
	ctx = process.WithManager(ctx, &mockManager{})
	task := runtime.Task{}

	_, err := handler.Call(ctx, task)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "register caller pid")
}

func TestProcessHandler_Call_AttachRelayError(t *testing.T) {
	handler := &processHandler{
		log:       zap.NewNop(),
		pidGen:    newTestPIDGen(),
		processID: registry.NewID("test", "proc1"),
		hostID:    "test-host",
	}

	node := &mockNode{attachErr: assert.AnError}
	topo := &mockTopology{}

	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	ctx = relayapi.WithNode(ctx, node)
	ctx = topology.WithTopology(ctx, topo)
	ctx = process.WithManager(ctx, &mockManager{})
	task := runtime.Task{}

	_, err := handler.Call(ctx, task)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "attach to relay")
}

func TestProcessHandler_Call_StartProcessError(t *testing.T) {
	handler := &processHandler{
		log:       zap.NewNop(),
		pidGen:    newTestPIDGen(),
		processID: registry.NewID("test", "proc1"),
		hostID:    "test-host",
	}

	node := &mockNode{}
	topo := &mockTopology{}
	mgr := &mockManager{startErr: assert.AnError}

	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	ctx = relayapi.WithNode(ctx, node)
	ctx = topology.WithTopology(ctx, topo)
	ctx = process.WithManager(ctx, mgr)
	task := runtime.Task{}

	_, err := handler.Call(ctx, task)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start process")
}

func TestProcessHandler_Call_ContextCanceled(t *testing.T) {
	handler := &processHandler{
		log:       zap.NewNop(),
		pidGen:    newTestPIDGen(),
		processID: registry.NewID("test", "proc1"),
		hostID:    "test-host",
	}

	node := &mockNode{}
	topo := &mockTopology{}
	mgr := &mockManager{startPID: pid.PID{Node: "test", Host: "test-host", UniqID: "123"}}

	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	ctx, cancel := context.WithCancel(ctx)
	ctx = relayapi.WithNode(ctx, node)
	ctx = topology.WithTopology(ctx, topo)
	ctx = process.WithManager(ctx, mgr)
	task := runtime.Task{}

	// Cancel immediately to trigger context cancellation path
	cancel()

	result, err := handler.Call(ctx, task)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, context.Canceled, result.Error)
}

func TestNewListener(t *testing.T) {
	bus := eventbus.NewBus()
	pidGen := newTestPIDGen()
	log := zap.NewNop()

	l := NewListener(log, bus, pidGen)

	assert.NotNil(t, l)
	assert.Equal(t, log, l.log)
	assert.Equal(t, bus, l.bus)
	assert.Equal(t, pidGen, l.pidGen)
	assert.NotNil(t, l.registered)
}

// Mock implementations

type mockNode struct {
	attachErr error
}

func (m *mockNode) ID() pid.NodeID                                       { return "test-node" }
func (m *mockNode) Send(_ *relayapi.Package) error                       { return nil }
func (m *mockNode) Detach(_ pid.PID)                                     {}
func (m *mockNode) RegisterHost(_ pid.HostID, _ relayapi.Receiver) error { return nil }
func (m *mockNode) UnregisterHost(_ pid.HostID)                          {}
func (m *mockNode) GetHost(_ pid.HostID) (relayapi.Receiver, bool)       { return nil, false }

func (m *mockNode) Attach(_ pid.PID, _ chan *relayapi.Package) (context.CancelFunc, error) {
	if m.attachErr != nil {
		return nil, m.attachErr
	}
	return func() {}, nil
}

type mockTopology struct {
	registerErr error
}

func (m *mockTopology) Register(_ pid.PID) error {
	return m.registerErr
}
func (m *mockTopology) Remove(_ pid.PID)                    {}
func (m *mockTopology) Notify(_ pid.PID, _ *runtime.Result) {}
func (m *mockTopology) Monitor(_, _ pid.PID) error          { return nil }
func (m *mockTopology) Demonitor(_, _ pid.PID) error        { return nil }
func (m *mockTopology) Link(_, _ pid.PID) error             { return nil }
func (m *mockTopology) Unlink(_, _ pid.PID) error           { return nil }
func (m *mockTopology) GetLinks(_ pid.PID) []pid.PID        { return nil }

type mockManager struct {
	startPID pid.PID
	startErr error
}

func (m *mockManager) Start(_ context.Context, _ *process.Start) (pid.PID, error) {
	if m.startErr != nil {
		return pid.PID{}, m.startErr
	}
	return m.startPID, nil
}
func (m *mockManager) Cancel(_ context.Context, _, _ pid.PID, _ time.Time) error { return nil }
func (m *mockManager) Terminate(_ context.Context, _ pid.PID) error              { return nil }
