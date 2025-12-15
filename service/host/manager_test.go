package host

import (
	"context"
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

type mockCommandRegistry struct{}

func (m *mockCommandRegistry) Get(dispatcherapi.CommandID) dispatcherapi.Handler {
	return nil
}

func (m *mockCommandRegistry) Has(dispatcherapi.CommandID) bool {
	return false
}

func newTestManager(*testing.T) *Manager {
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

func TestNewManager(t *testing.T) {
	mgr := newTestManager(t)
	assert.NotNil(t, mgr)
	assert.NotNil(t, mgr.hosts)
}

func TestManager_Add(t *testing.T) {
	mgr := newTestManager(t)
	entry := makeHostEntry(registry.NewID("test", "host1"))

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	_, ok := mgr.hosts[entry.ID]
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
	assert.Equal(t, "failed to decode host config", hostErr.Error())
}

func TestManager_Delete(t *testing.T) {
	mgr := newTestManager(t)
	entry := makeHostEntry(registry.NewID("test", "host1"))

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	err = mgr.Delete(context.Background(), entry)
	require.NoError(t, err)

	_, ok := mgr.hosts[entry.ID]
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

func TestManager_Update(t *testing.T) {
	mgr := newTestManager(t)
	entry := makeHostEntry(registry.NewID("test", "host1"))

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	err = mgr.Update(context.Background(), entry)
	require.NoError(t, err)
}

func TestManager_GetHost(t *testing.T) {
	mgr := newTestManager(t)
	entry := makeHostEntry(registry.NewID("test", "host1"))

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	h, ok := mgr.GetHost("test:host1")
	assert.True(t, ok)
	assert.NotNil(t, h)

	h, ok = mgr.GetHost("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, h)
}

func TestCompositeLifecycle_OnStart(t *testing.T) {
	var globalCalled, hostCalled bool

	global := &testLifecycle{onStart: func() { globalCalled = true }}
	hostLC := &testLifecycle{onStart: func() { hostCalled = true }}

	c := &compositeLifecycle{global: global, host: hostLC}
	c.OnStart(context.Background(), pid.PID{}, nil)

	assert.True(t, globalCalled)
	assert.True(t, hostCalled)
}

func TestCompositeLifecycle_OnComplete(t *testing.T) {
	var globalCalled, hostCalled bool

	global := &testLifecycle{onComplete: func() { globalCalled = true }}
	hostLC := &testLifecycle{onComplete: func() { hostCalled = true }}

	c := &compositeLifecycle{global: global, host: hostLC}
	c.OnComplete(context.Background(), pid.PID{}, nil)

	assert.True(t, globalCalled)
	assert.True(t, hostCalled)
}

type testLifecycle struct {
	onStart    func()
	onComplete func()
}

func (t *testLifecycle) OnStart(context.Context, pid.PID, process.Process) {
	if t.onStart != nil {
		t.onStart()
	}
}

func (t *testLifecycle) OnComplete(context.Context, pid.PID, *runtime.Result) {
	if t.onComplete != nil {
		t.onComplete()
	}
}

func TestManager_ImplementsEntryListener(_ *testing.T) {
	var _ registry.EntryListener = (*Manager)(nil)
}
