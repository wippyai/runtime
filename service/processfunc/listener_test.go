package processfunc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// mockProcessManager implements process.Manager for testing
type mockProcessManager struct {
	startFunc           func(ctx context.Context, start *process.Start) (relay.PID, error)
	cancelFunc          func(ctx context.Context, from relay.PID, to relay.PID, deadline time.Time) error
	terminateFunc       func(ctx context.Context, pid relay.PID) error
	attachLifecycleFunc func(context.Context, process.Lifecycle) context.Context
}

func (m *mockProcessManager) Start(ctx context.Context, start *process.Start) (relay.PID, error) {
	if m.startFunc != nil {
		return m.startFunc(ctx, start)
	}
	return relay.PID{}, fmt.Errorf("not implemented")
}

func (m *mockProcessManager) Cancel(ctx context.Context, from relay.PID, to relay.PID, deadline time.Time) error {
	if m.cancelFunc != nil {
		return m.cancelFunc(ctx, from, to, deadline)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockProcessManager) Terminate(ctx context.Context, pid relay.PID) error {
	if m.terminateFunc != nil {
		return m.terminateFunc(ctx, pid)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockProcessManager) AttachLifecycle(ctx context.Context, lc process.Lifecycle) context.Context {
	if m.attachLifecycleFunc != nil {
		return m.attachLifecycleFunc(ctx, lc)
	}
	return ctx
}

func TestListener_NewListener(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	defer bus.Stop()

	procs := &mockProcessManager{}

	listener := NewListener(logger, bus, procs)
	assert.NotNil(t, listener)
	assert.NotNil(t, listener.log)
	assert.NotNil(t, listener.bus)
	assert.NotNil(t, listener.procs)
	assert.NotNil(t, listener.uniqID)
	assert.NotNil(t, listener.registered)
	assert.Empty(t, listener.registered)
}

func TestListener_ProcessEntry_Create(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	defer bus.Stop()

	procs := &mockProcessManager{}
	listener := NewListener(logger, bus, procs)

	ctx := context.Background()

	// Subscribe to function.register events
	ch := make(chan event.Event, 10)
	subID, err := bus.SubscribeP(ctx, function.System, function.Register, ch)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	// Process entry with default_host
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "proc1"},
		Kind: "process.test",
		Meta: map[string]any{
			"default_host": "host1",
		},
	}

	listener.processEntry(ctx, registry.Create, entry)

	// Should receive function registration event
	select {
	case evt := <-ch:
		assert.Equal(t, function.System, evt.System)
		assert.Equal(t, function.Register, evt.Kind)
		assert.Equal(t, "test:proc1", evt.Path)

		funcEntry, ok := evt.Data.(*function.FuncEntry)
		require.True(t, ok)
		assert.NotNil(t, funcEntry.Handler)
		assert.NotNil(t, funcEntry.Options)
		assert.Equal(t, "host1", funcEntry.Options.GetString("default_host", ""))
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for function register event")
	}

	// Check registered map
	assert.Contains(t, listener.registered, "test:proc1")
	assert.Equal(t, relay.HostID("host1"), listener.registered["test:proc1"])
}

func TestListener_ProcessEntry_CreateWithoutDefaultHost(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	defer bus.Stop()

	procs := &mockProcessManager{}
	listener := NewListener(logger, bus, procs)

	ctx := context.Background()

	// Subscribe to all function events
	ch := make(chan event.Event, 10)
	subID, err := bus.Subscribe(ctx, function.System, ch)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	// Process entry without default_host
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "proc1"},
		Kind: "process.test",
		Meta: map[string]any{},
	}

	listener.processEntry(ctx, registry.Create, entry)

	// Should NOT receive any function event
	select {
	case <-ch:
		t.Fatal("should not register function without default_host")
	case <-time.After(50 * time.Millisecond):
		// Expected
	}

	// Check registered map is empty
	assert.Empty(t, listener.registered)
}

func TestListener_ProcessEntry_CreateNonProcessKind(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	defer bus.Stop()

	procs := &mockProcessManager{}
	listener := NewListener(logger, bus, procs)

	ctx := context.Background()

	// Subscribe to all function events
	ch := make(chan event.Event, 10)
	subID, err := bus.Subscribe(ctx, function.System, ch)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	// Entry with non-process kind
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "func1"},
		Kind: "function.lua",
		Meta: map[string]any{
			"default_host": "host1",
		},
	}

	listener.processEntry(ctx, registry.Create, entry)

	// Should NOT receive any function event
	select {
	case <-ch:
		t.Fatal("should not register non-process entries")
	case <-time.After(50 * time.Millisecond):
		// Expected
	}

	assert.Empty(t, listener.registered)
}

func TestListener_ProcessEntry_Update(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	defer bus.Stop()

	procs := &mockProcessManager{}
	listener := NewListener(logger, bus, procs)

	ctx := context.Background()

	// Pre-register a function
	listener.registered["test:proc1"] = "host1"

	// Subscribe to all function events
	ch := make(chan event.Event, 10)
	subID, err := bus.Subscribe(ctx, function.System, ch)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	// Update with new host
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "proc1"},
		Kind: "process.test",
		Meta: map[string]any{
			"default_host": "host2",
		},
	}

	listener.processEntry(ctx, registry.Update, entry)

	// Should receive delete then register events
	events := []event.Event{}
	timeout := time.After(200 * time.Millisecond)
	for i := 0; i < 2; i++ {
		select {
		case evt := <-ch:
			events = append(events, evt)
		case <-timeout:
			t.Fatalf("timeout waiting for event %d", i+1)
		}
	}

	// First event should be delete
	assert.Equal(t, function.Delete, events[0].Kind)
	assert.Equal(t, "test:proc1", events[0].Path)

	// Second event should be register with new host
	assert.Equal(t, function.Register, events[1].Kind)
	assert.Equal(t, "test:proc1", events[1].Path)

	// Check updated host
	assert.Equal(t, relay.HostID("host2"), listener.registered["test:proc1"])
}

func TestListener_ProcessEntry_UpdateRemoveHost(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	defer bus.Stop()

	procs := &mockProcessManager{}
	listener := NewListener(logger, bus, procs)

	ctx := context.Background()

	// Pre-register a function
	listener.registered["test:proc1"] = "host1"

	// Subscribe to all function events
	ch := make(chan event.Event, 10)
	subID, err := bus.Subscribe(ctx, function.System, ch)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	// Update without default_host (removes it)
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "proc1"},
		Kind: "process.test",
		Meta: map[string]any{},
	}

	listener.processEntry(ctx, registry.Update, entry)

	// Should receive delete event only
	select {
	case evt := <-ch:
		assert.Equal(t, function.Delete, evt.Kind)
		assert.Equal(t, "test:proc1", evt.Path)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for delete event")
	}

	// Check function unregistered
	assert.NotContains(t, listener.registered, "test:proc1")
}

func TestListener_ProcessEntry_Delete(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	defer bus.Stop()

	procs := &mockProcessManager{}
	listener := NewListener(logger, bus, procs)

	ctx := context.Background()

	// Pre-register a function
	listener.registered["test:proc1"] = "host1"

	// Subscribe to all function events
	ch := make(chan event.Event, 10)
	subID, err := bus.Subscribe(ctx, function.System, ch)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	// Delete entry
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "proc1"},
		Kind: "process.test",
	}

	listener.processEntry(ctx, registry.Delete, entry)

	// Should receive delete event
	select {
	case evt := <-ch:
		assert.Equal(t, function.Delete, evt.Kind)
		assert.Equal(t, "test:proc1", evt.Path)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for delete event")
	}

	// Check function unregistered
	assert.NotContains(t, listener.registered, "test:proc1")
}

func TestListener_ProcessEntry_DeleteNotRegistered(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	defer bus.Stop()

	procs := &mockProcessManager{}
	listener := NewListener(logger, bus, procs)

	ctx := context.Background()

	// Subscribe to all function events
	ch := make(chan event.Event, 10)
	subID, err := bus.Subscribe(ctx, function.System, ch)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	// Delete entry that wasn't registered
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "proc1"},
		Kind: "process.test",
	}

	listener.processEntry(ctx, registry.Delete, entry)

	// Should NOT receive any event
	select {
	case <-ch:
		t.Fatal("should not send delete for non-registered function")
	case <-time.After(50 * time.Millisecond):
		// Expected
	}
}

func TestWithProcessFunctionBridge(t *testing.T) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	defer bus.Stop()

	procs := &mockProcessManager{}

	handler := WithProcessFunctionBridge(logger, bus, procs)
	assert.NotNil(t, handler)

	// Test handler responds to registry events
	ctx := context.Background()

	// Subscribe to all function events
	ch := make(chan event.Event, 10)
	subID, err := bus.Subscribe(ctx, function.System, ch)
	require.NoError(t, err)
	defer bus.Unsubscribe(ctx, subID)

	// Send registry event
	err = handler.Handle(ctx, event.Event{
		System: registry.System,
		Kind:   registry.Create,
		Path:   "test.proc1",
		Data: registry.Entry{
			ID:   registry.ID{NS: "test", Name: "proc1"},
			Kind: "process.test",
			Meta: map[string]any{
				"default_host": "host1",
			},
		},
	})
	require.NoError(t, err)

	// Should receive function register event
	select {
	case evt := <-ch:
		assert.Equal(t, function.Register, evt.Kind)
		assert.Equal(t, "test:proc1", evt.Path)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for function register event")
	}
}
