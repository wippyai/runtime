package host

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/service/host"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/system/eventbus"
	payloadSystem "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"go.uber.org/zap"
)

// --- Test Infrastructure for Manager ---

type mockCommandRegistry struct{}

func (m *mockCommandRegistry) Get(_ dispatcherapi.CommandID) dispatcherapi.Handler {
	return nil
}

func (m *mockCommandRegistry) Has(_ dispatcherapi.CommandID) bool {
	return false
}

func newTestManager(_ *testing.T) *Manager {
	bus := eventbus.NewBus()
	dtt := payloadSystem.GlobalTranscoder()
	json.Register(dtt)
	cmdReg := &mockCommandRegistry{}
	factory := &mockFactory{proc: &mockProcess{}}
	pidGen := uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test-node")
	log := zap.NewNop()
	return NewManager(bus, dtt, cmdReg, factory, pidGen, log)
}

func makeHostEntry(id registry.ID) registry.Entry {
	return registry.Entry{
		ID:   id,
		Kind: host.Host,
		Meta: attrs.NewBag(),
		Data: payload.New(map[string]any{
			"host": map[string]any{
				"workers":    2,
				"queue_size": 1024,
			},
		}),
	}
}

// --- Manager Construction Tests ---

func TestNewManager(t *testing.T) {
	mgr := newTestManager(t)
	assert.NotNil(t, mgr)
	assert.NotNil(t, mgr.hosts)
	assert.NotNil(t, mgr.bus)
	assert.NotNil(t, mgr.dtt)
	assert.NotNil(t, mgr.factory)
	assert.NotNil(t, mgr.pidGen)
}

// --- Manager Add Tests ---

func TestManager_Add(t *testing.T) {
	mgr := newTestManager(t)
	entry := makeHostEntry(registry.NewID("test", "host1"))

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	mgr.mu.RLock()
	_, ok := mgr.hosts[entry.ID]
	mgr.mu.RUnlock()
	assert.True(t, ok)
}

func TestManager_Add_DecodeError(t *testing.T) {
	mgr := newTestManager(t)

	entry := registry.Entry{
		ID:   registry.NewID("test", "host1"),
		Kind: host.Host,
		Meta: attrs.NewBag(),
		Data: payload.New([]byte("invalid json {")),
	}

	err := mgr.Add(context.Background(), entry)
	require.Error(t, err)

	var hostErr apierror.Error
	require.ErrorAs(t, err, &hostErr)
	assert.Contains(t, hostErr.Error(), "failed to decode host config")
	assert.NotEmpty(t, hostErr.Details().GetString("cause", ""))
}

func TestManager_Add_InvalidKind(t *testing.T) {
	mgr := newTestManager(t)

	entry := registry.Entry{
		ID:   registry.NewID("test", "host1"),
		Kind: "invalid.kind",
		Meta: attrs.NewBag(),
		Data: payload.New(map[string]any{}),
	}

	err := mgr.Add(context.Background(), entry)
	require.Error(t, err)

	var hostErr apierror.Error
	require.ErrorAs(t, err, &hostErr)
	assert.Contains(t, hostErr.Error(), "unsupported entry kind")
	assert.Equal(t, "invalid.kind", hostErr.Details().GetString("kind", ""))
}

func TestManager_Add_MultipleHosts(t *testing.T) {
	mgr := newTestManager(t)

	entry1 := makeHostEntry(registry.NewID("test", "host1"))
	entry2 := makeHostEntry(registry.NewID("test", "host2"))

	require.NoError(t, mgr.Add(context.Background(), entry1))
	require.NoError(t, mgr.Add(context.Background(), entry2))

	mgr.mu.RLock()
	assert.Len(t, mgr.hosts, 2)
	mgr.mu.RUnlock()
}

// --- Manager Delete Tests ---

func TestManager_Delete(t *testing.T) {
	mgr := newTestManager(t)
	entry := makeHostEntry(registry.NewID("test", "host1"))

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	err = mgr.Delete(context.Background(), entry)
	require.NoError(t, err)

	mgr.mu.RLock()
	_, ok := mgr.hosts[entry.ID]
	mgr.mu.RUnlock()
	assert.False(t, ok)
}

func TestManager_Delete_NotFound(t *testing.T) {
	mgr := newTestManager(t)

	entry := registry.Entry{
		ID:   registry.NewID("test", "nonexistent"),
		Kind: host.Host,
	}

	err := mgr.Delete(context.Background(), entry)
	require.NoError(t, err)
}

func TestManager_Delete_InvalidKind(t *testing.T) {
	mgr := newTestManager(t)

	entry := registry.Entry{
		ID:   registry.NewID("test", "host1"),
		Kind: "invalid.kind",
	}

	err := mgr.Delete(context.Background(), entry)
	require.Error(t, err)

	var hostErr apierror.Error
	require.ErrorAs(t, err, &hostErr)
	assert.Contains(t, hostErr.Error(), "unsupported entry kind")
	assert.Equal(t, "invalid.kind", hostErr.Details().GetString("kind", ""))
}

func TestManager_Delete_StopsHost(t *testing.T) {
	mgr := newTestManager(t)
	entry := makeHostEntry(registry.NewID("test", "host1"))

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	// Start the host
	mgr.mu.RLock()
	h := mgr.hosts[entry.ID]
	mgr.mu.RUnlock()
	_, err = h.Start(ctxWithAppContext())
	require.NoError(t, err)

	// Delete should stop it
	err = mgr.Delete(context.Background(), entry)
	require.NoError(t, err)

	assert.False(t, h.running.Load())
}

// --- Manager Update Tests ---

func TestManager_Update(t *testing.T) {
	mgr := newTestManager(t)
	entry := makeHostEntry(registry.NewID("test", "host1"))

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	err = mgr.Update(context.Background(), entry)
	require.NoError(t, err)

	mgr.mu.RLock()
	_, ok := mgr.hosts[entry.ID]
	mgr.mu.RUnlock()
	assert.True(t, ok)
}

func TestManager_Update_InvalidKind(t *testing.T) {
	mgr := newTestManager(t)

	entry := registry.Entry{
		ID:   registry.NewID("test", "host1"),
		Kind: "invalid.kind",
		Meta: attrs.NewBag(),
		Data: payload.New(map[string]any{}),
	}

	err := mgr.Update(context.Background(), entry)
	require.Error(t, err)

	var hostErr apierror.Error
	require.ErrorAs(t, err, &hostErr)
	assert.Contains(t, hostErr.Error(), "unsupported entry kind")
	assert.Equal(t, "invalid.kind", hostErr.Details().GetString("kind", ""))
}

func TestManager_Update_NonExistent(t *testing.T) {
	mgr := newTestManager(t)
	entry := makeHostEntry(registry.NewID("test", "host1"))

	// Update on non-existent should add
	err := mgr.Update(context.Background(), entry)
	require.NoError(t, err)

	mgr.mu.RLock()
	_, ok := mgr.hosts[entry.ID]
	mgr.mu.RUnlock()
	assert.True(t, ok)
}

func TestManager_Update_DecodeError(t *testing.T) {
	mgr := newTestManager(t)

	// First add a valid host
	validEntry := makeHostEntry(registry.NewID("test", "host1"))
	require.NoError(t, mgr.Add(context.Background(), validEntry))

	// Then try to update with invalid data
	invalidEntry := registry.Entry{
		ID:   registry.NewID("test", "host1"),
		Kind: host.Host,
		Meta: attrs.NewBag(),
		Data: payload.New([]byte("invalid json {")),
	}

	err := mgr.Update(context.Background(), invalidEntry)
	require.Error(t, err)
}

// --- Manager GetHost Tests ---

func TestManager_GetHost(t *testing.T) {
	mgr := newTestManager(t)
	entry := makeHostEntry(registry.NewID("test", "host1"))

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	h, ok := mgr.GetHost("test:host1")
	assert.True(t, ok)
	assert.NotNil(t, h)
}

func TestManager_GetHost_NotFound(t *testing.T) {
	mgr := newTestManager(t)

	h, ok := mgr.GetHost("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, h)
}

func TestManager_GetHost_InvalidID(t *testing.T) {
	mgr := newTestManager(t)

	h, ok := mgr.GetHost("")
	assert.False(t, ok)
	assert.Nil(t, h)
}

// --- CompositeLifecycle Tests ---

func TestCompositeLifecycle_OnStart(t *testing.T) {
	var globalCalled, hostCalled bool

	global := &mockLifecycle{onStartFunc: func(context.Context, pid.PID, process.Process) { globalCalled = true }}
	hostLC := &mockLifecycle{onStartFunc: func(context.Context, pid.PID, process.Process) { hostCalled = true }}

	c := &compositeLifecycle{global: global, host: hostLC}
	err := c.OnStart(context.Background(), pid.PID{}, nil)

	require.NoError(t, err)
	assert.True(t, globalCalled)
	assert.True(t, hostCalled)
}

func TestCompositeLifecycle_OnStart_GlobalError(t *testing.T) {
	globalErr := errors.New("global lifecycle error")
	var hostCalled bool

	global := &mockLifecycle{onStartErr: globalErr}
	hostLC := &mockLifecycle{onStartFunc: func(context.Context, pid.PID, process.Process) { hostCalled = true }}

	c := &compositeLifecycle{global: global, host: hostLC}
	err := c.OnStart(context.Background(), pid.PID{}, nil)

	assert.ErrorIs(t, err, globalErr)
	assert.False(t, hostCalled) // host should not be called if global fails
}

func TestCompositeLifecycle_OnStart_HostError(t *testing.T) {
	hostErr := errors.New("host lifecycle error")
	var globalCalled bool

	global := &mockLifecycle{onStartFunc: func(context.Context, pid.PID, process.Process) { globalCalled = true }}
	hostLC := &mockLifecycle{onStartErr: hostErr}

	c := &compositeLifecycle{global: global, host: hostLC}
	err := c.OnStart(context.Background(), pid.PID{}, nil)

	assert.ErrorIs(t, err, hostErr)
	assert.True(t, globalCalled) // global should be called before host fails
}

func TestCompositeLifecycle_OnComplete(t *testing.T) {
	var globalCalled, hostCalled bool

	global := &mockLifecycle{onComplete: func(context.Context, pid.PID, *runtime.Result) { globalCalled = true }}
	hostLC := &mockLifecycle{onComplete: func(context.Context, pid.PID, *runtime.Result) { hostCalled = true }}

	c := &compositeLifecycle{global: global, host: hostLC}
	c.OnComplete(context.Background(), pid.PID{}, nil)

	assert.True(t, globalCalled)
	assert.True(t, hostCalled)
}

func TestCompositeLifecycle_OnComplete_Order(t *testing.T) {
	order := make([]string, 0, 2)

	global := &mockLifecycle{onComplete: func(context.Context, pid.PID, *runtime.Result) { order = append(order, "global") }}
	hostLC := &mockLifecycle{onComplete: func(context.Context, pid.PID, *runtime.Result) { order = append(order, "host") }}

	c := &compositeLifecycle{global: global, host: hostLC}
	c.OnComplete(context.Background(), pid.PID{}, nil)

	assert.Equal(t, []string{"global", "host"}, order)
}

func TestCompositeLifecycle_OnStart_Order(t *testing.T) {
	order := make([]string, 0, 2)

	global := &mockLifecycle{onStartFunc: func(context.Context, pid.PID, process.Process) { order = append(order, "global") }}
	hostLC := &mockLifecycle{onStartFunc: func(context.Context, pid.PID, process.Process) { order = append(order, "host") }}

	c := &compositeLifecycle{global: global, host: hostLC}
	err := c.OnStart(context.Background(), pid.PID{}, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"global", "host"}, order)
}

// --- Interface Compliance ---

var _ registry.EntryListener = (*Manager)(nil)

var _ dispatcherapi.Registry = (*mockCommandRegistry)(nil)
