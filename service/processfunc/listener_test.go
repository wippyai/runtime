// SPDX-License-Identifier: MPL-2.0

package processfunc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	topapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func newTestPIDGen() *uniqid.PIDGenerator {
	return uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test-node")
}

const testTimeout = 100 * time.Millisecond

func waitForEvent(t *testing.T, ch <-chan event.Event) event.Event {
	select {
	case e := <-ch:
		return e
	case <-time.After(testTimeout):
		t.Fatal("timeout waiting for event")
		return event.Event{}
	}
}

func newTestListener(t *testing.T) (*Listener, <-chan event.Event) {
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
	return l, events
}

func TestListener_Add_WithDefaultHost(t *testing.T) {
	l, events := newTestListener(t)

	meta := attrs.NewBag()
	meta.Set("default_host", "test-host")

	entry := registry.Entry{
		ID:   registry.NewID("test", "proc1"),
		Kind: "process.lua",
		Meta: meta,
	}

	err := l.Add(context.Background(), entry)
	require.NoError(t, err)

	evt := waitForEvent(t, events)
	assert.Equal(t, function.System, evt.System)
	assert.Equal(t, function.FunctionRegister, evt.Kind)
	assert.Equal(t, "test:proc1", evt.Path)

	l.mu.RLock()
	_, exists := l.registered["test:proc1"]
	l.mu.RUnlock()
	assert.True(t, exists)
}

func TestListener_Add_WithoutDefaultHost(t *testing.T) {
	l, events := newTestListener(t)

	entry := registry.Entry{
		ID:   registry.NewID("test", "proc1"),
		Kind: "process.lua",
		Meta: attrs.NewBag(),
	}

	err := l.Add(context.Background(), entry)
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
	l, events := newTestListener(t)

	entry := registry.Entry{
		ID:   registry.NewID("test", "svc1"),
		Kind: "service.http",
		Meta: attrs.NewBag(),
	}

	err := l.Add(context.Background(), entry)
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
	l, events := newTestListener(t)

	meta := attrs.NewBag()
	meta.Set("default_host", "host1")

	entry := registry.Entry{
		ID:   registry.NewID("test", "proc1"),
		Kind: "process.lua",
		Meta: meta,
	}

	err := l.Add(context.Background(), entry)
	require.NoError(t, err)

	// Consume the Add event
	waitForEvent(t, events)

	// Update with different host
	meta2 := attrs.NewBag()
	meta2.Set("default_host", "host2")
	entry.Meta = meta2

	err = l.Update(context.Background(), entry)
	require.NoError(t, err)

	// Should receive Delete then Register
	evt1 := waitForEvent(t, events)
	assert.Equal(t, function.FunctionDelete, evt1.Kind)

	evt2 := waitForEvent(t, events)
	assert.Equal(t, function.FunctionRegister, evt2.Kind)
}

func TestListener_Delete(t *testing.T) {
	l, events := newTestListener(t)

	meta := attrs.NewBag()
	meta.Set("default_host", "test-host")

	entry := registry.Entry{
		ID:   registry.NewID("test", "proc1"),
		Kind: "process.lua",
		Meta: meta,
	}

	err := l.Add(context.Background(), entry)
	require.NoError(t, err)

	// Consume the Add event
	waitForEvent(t, events)

	err = l.Delete(context.Background(), entry)
	require.NoError(t, err)

	evt := waitForEvent(t, events)
	assert.Equal(t, function.System, evt.System)
	assert.Equal(t, function.FunctionDelete, evt.Kind)

	l.mu.RLock()
	_, exists := l.registered["test:proc1"]
	l.mu.RUnlock()
	assert.False(t, exists)
}

func TestListener_OptionsFromMeta(t *testing.T) {
	l, events := newTestListener(t)

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

	err := l.Add(context.Background(), entry)
	require.NoError(t, err)

	evt := waitForEvent(t, events)
	funcEntry, ok := evt.Data.(*function.FuncEntry)
	require.True(t, ok)
	assert.NotNil(t, funcEntry.Options)
	assert.Equal(t, "30s", funcEntry.Options.GetString("timeout", ""))
}

func TestListener_HostInMetaOptionsPreserved(t *testing.T) {
	l, events := newTestListener(t)

	// Options bag exists with timeout but NOT default_host
	options := attrs.NewBag()
	options.Set("timeout", "60s")
	options.Set("retry", "3")

	meta := attrs.NewBag()
	meta.Set("options", options)
	meta.Set("default_host", "meta-host") // default_host in meta, not options

	entry := registry.Entry{
		ID:   registry.NewID("test", "proc1"),
		Kind: "process.lua",
		Meta: meta,
	}

	err := l.Add(context.Background(), entry)
	require.NoError(t, err)

	evt := waitForEvent(t, events)
	funcEntry, ok := evt.Data.(*function.FuncEntry)
	require.True(t, ok)
	require.NotNil(t, funcEntry.Options)

	// Both original options AND default_host should be present
	assert.Equal(t, "60s", funcEntry.Options.GetString("timeout", ""))
	assert.Equal(t, "3", funcEntry.Options.GetString("retry", ""))
	assert.Equal(t, "meta-host", funcEntry.Options.GetString("default_host", ""))
}

func TestListener_Update_NoChange(t *testing.T) {
	l, events := newTestListener(t)

	meta := attrs.NewBag()
	meta.Set("default_host", "test-host")

	entry := registry.Entry{
		ID:   registry.NewID("test", "proc1"),
		Kind: "process.lua",
		Meta: meta,
	}

	err := l.Add(context.Background(), entry)
	require.NoError(t, err)

	// Consume the Add event
	waitForEvent(t, events)

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
	l, events := newTestListener(t)

	entry := registry.Entry{
		ID:   registry.NewID("test", "proc1"),
		Kind: "process.lua",
		Meta: attrs.NewBag(),
	}

	// Delete without prior registration
	err := l.Delete(context.Background(), entry)
	require.NoError(t, err)

	// No events should be sent
	select {
	case <-events:
		t.Fatal("unexpected event")
	case <-time.After(10 * time.Millisecond):
		// Expected - no event
	}
}

var _ registry.EntryListener = (*Listener)(nil)

// processHandler.Call tests

func TestProcessHandler_Call_RegisterPIDError(t *testing.T) {
	topo := &mockTopology{registerErr: assert.AnError}
	handler := &processHandler{
		log:       zap.NewNop(),
		pidGen:    newTestPIDGen(),
		node:      &mockNode{},
		topo:      topo,
		manager:   &mockManager{},
		processID: "test:proc1",
		hostID:    "test-host",
	}

	_, err := handler.Call(context.Background(), runtime.Task{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "register caller pid")
}

func TestProcessHandler_Call_AttachRelayError(t *testing.T) {
	handler := &processHandler{
		log:       zap.NewNop(),
		pidGen:    newTestPIDGen(),
		node:      &mockNode{attachErr: assert.AnError},
		topo:      &mockTopology{},
		manager:   &mockManager{},
		processID: "test:proc1",
		hostID:    "test-host",
	}

	_, err := handler.Call(context.Background(), runtime.Task{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "attach to relay")
}

func TestProcessHandler_Call_StartProcessError(t *testing.T) {
	handler := &processHandler{
		log:       zap.NewNop(),
		pidGen:    newTestPIDGen(),
		node:      &mockNode{},
		topo:      &mockTopology{},
		manager:   &mockManager{startErr: assert.AnError},
		processID: "test:proc1",
		hostID:    "test-host",
	}

	_, err := handler.Call(context.Background(), runtime.Task{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start process")
}

func TestProcessHandler_Call_ContextCanceled(t *testing.T) {
	handler := &processHandler{
		log:       zap.NewNop(),
		pidGen:    newTestPIDGen(),
		node:      &mockNode{},
		topo:      &mockTopology{},
		manager:   &mockManager{startPID: pid.PID{Node: "test", Host: "test-host", UniqID: "123"}},
		processID: "test:proc1",
		hostID:    "test-host",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := handler.Call(ctx, runtime.Task{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, context.Canceled, result.Error)
}

func TestProcessHandler_Call_ExitEvent(t *testing.T) {
	exitCh := make(chan *relayapi.Package, 1)
	node := &mockNodeWithChannel{ch: exitCh}

	handler := &processHandler{
		log:       zap.NewNop(),
		pidGen:    newTestPIDGen(),
		node:      node,
		topo:      &mockTopology{},
		manager:   &mockManager{startPID: pid.PID{Node: "test", Host: "test-host", UniqID: "123"}},
		processID: "test:proc1",
		hostID:    "test-host",
	}

	expectedResult := &runtime.Result{Value: payload.New("success")}

	go func() {
		time.Sleep(10 * time.Millisecond)
		pkg := &relayapi.Package{}
		pkg.AddMessage(topapi.TopicEvents, payload.New(&topapi.ExitEvent{Result: expectedResult}))
		exitCh <- pkg
	}()

	result, err := handler.Call(context.Background(), runtime.Task{})
	require.NoError(t, err)
	assert.Equal(t, expectedResult, result)
}

func TestProcessHandler_Call_MonitorChannelClosed(t *testing.T) {
	exitCh := make(chan *relayapi.Package, 1)
	node := &mockNodeWithChannel{ch: exitCh}

	handler := &processHandler{
		log:       zap.NewNop(),
		pidGen:    newTestPIDGen(),
		node:      node,
		topo:      &mockTopology{},
		manager:   &mockManager{startPID: pid.PID{Node: "test", Host: "test-host", UniqID: "123"}},
		processID: "test:proc1",
		hostID:    "test-host",
	}

	go func() {
		time.Sleep(10 * time.Millisecond)
		close(exitCh)
	}()

	result, err := handler.Call(context.Background(), runtime.Task{})
	require.NoError(t, err)
	assert.Equal(t, ErrMonitorChannelClosed, result.Error)
}

func TestNewListener(t *testing.T) {
	bus := eventbus.NewBus()
	pidGen := newTestPIDGen()
	log := zap.NewNop()
	node := &mockNode{}
	topo := &mockTopology{}
	mgr := &mockManager{}

	l := NewListener(log, bus, pidGen, node, topo, mgr)

	assert.NotNil(t, l)
	assert.Equal(t, log, l.log)
	assert.Equal(t, bus, l.bus)
	assert.Equal(t, pidGen, l.pidGen)
	assert.Equal(t, node, l.node)
	assert.Equal(t, topo, l.topo)
	assert.Equal(t, mgr, l.manager)
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

type mockNodeWithChannel struct {
	ch chan *relayapi.Package
}

func (m *mockNodeWithChannel) ID() pid.NodeID                                       { return "test-node" }
func (m *mockNodeWithChannel) Send(_ *relayapi.Package) error                       { return nil }
func (m *mockNodeWithChannel) Detach(_ pid.PID)                                     {}
func (m *mockNodeWithChannel) RegisterHost(_ pid.HostID, _ relayapi.Receiver) error { return nil }
func (m *mockNodeWithChannel) UnregisterHost(_ pid.HostID)                          {}
func (m *mockNodeWithChannel) GetHost(_ pid.HostID) (relayapi.Receiver, bool)       { return nil, false }

func (m *mockNodeWithChannel) Attach(_ pid.PID, ch chan *relayapi.Package) (context.CancelFunc, error) {
	go func() {
		for pkg := range m.ch {
			ch <- pkg
		}
		close(ch)
	}()
	return func() {}, nil
}

type mockTopology struct {
	registerErr error
}

func (m *mockTopology) Register(_ pid.PID) error {
	return m.registerErr
}
func (m *mockTopology) Remove(_ pid.PID)                      {}
func (m *mockTopology) Complete(_ pid.PID, _ *runtime.Result) {}
func (m *mockTopology) Monitor(_, _ pid.PID) error            { return nil }
func (m *mockTopology) Demonitor(_, _ pid.PID) error          { return nil }
func (m *mockTopology) Link(_, _ pid.PID) error               { return nil }
func (m *mockTopology) Unlink(_, _ pid.PID) error             { return nil }
func (m *mockTopology) GetLinks(_ pid.PID) []pid.PID          { return nil }

type mockManager struct {
	startErr error
	startPID pid.PID
}

func (m *mockManager) Start(_ context.Context, _ *process.Start) (pid.PID, error) {
	if m.startErr != nil {
		return pid.PID{}, m.startErr
	}
	return m.startPID, nil
}
func (m *mockManager) Cancel(_ context.Context, _, _ pid.PID, _ string) error { return nil }
func (m *mockManager) Terminate(_ context.Context, _ pid.PID) error           { return nil }

func BenchmarkListener_Add(b *testing.B) {
	bus := eventbus.NewBus()
	pidGen := newTestPIDGen()
	log := zap.NewNop()
	node := &mockNode{}
	topo := &mockTopology{}
	mgr := &mockManager{}

	l := NewListener(log, bus, pidGen, node, topo, mgr)

	meta := attrs.NewBag()
	meta.Set("default_host", "test-host")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry := registry.Entry{
			ID:   registry.NewID("test", "proc1"),
			Kind: "process.lua",
			Meta: meta,
		}
		_ = l.Add(context.Background(), entry)
	}
}

func BenchmarkProcessHandler_Call(b *testing.B) {
	handler := &processHandler{
		log:       zap.NewNop(),
		pidGen:    newTestPIDGen(),
		node:      &mockNode{},
		topo:      &mockTopology{},
		manager:   &mockManager{startPID: pid.PID{Node: "test", Host: "test-host", UniqID: "123"}},
		processID: "test:proc1",
		hostID:    "test-host",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = handler.Call(ctx, runtime.Task{})
	}
}
