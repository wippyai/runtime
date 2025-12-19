package terminal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/service/terminal"
	"go.uber.org/zap"
)

type mockBus struct {
	events []event.Event
}

func (m *mockBus) Send(_ context.Context, e event.Event) {
	m.events = append(m.events, e)
}

func (m *mockBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockBus) Unsubscribe(context.Context, event.SubscriberID) {}

type mockTranscoder struct {
	shouldFail bool
}

func (m *mockTranscoder) Unmarshal(_ payload.Payload, out any) error {
	if m.shouldFail {
		return assert.AnError
	}
	if cfg, ok := out.(*terminal.HostConfig); ok {
		cfg.HideLogs = false
	}
	return nil
}

func (m *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

type mockFactory struct{}

func (m *mockFactory) Create(_ registry.ID) (process.Process, *process.Meta, error) {
	return &mockProcess{}, nil, nil
}

type mockProcess struct{}

func (m *mockProcess) Init(context.Context, string, payload.Payloads) error {
	return nil
}

func (m *mockProcess) Step([]process.Event, *process.StepOutput) error {
	return nil
}

func (m *mockProcess) Close() {}

type mockCommandRegistry struct{}

func (m *mockCommandRegistry) Get(dispatcherapi.CommandID) dispatcherapi.Handler {
	return nil
}

func (m *mockCommandRegistry) Has(dispatcherapi.CommandID) bool {
	return false
}

func TestNewManager(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{}
	cmdReg := &mockCommandRegistry{}
	factory := &mockFactory{}
	log := zap.NewNop()

	mgr := NewManager(bus, dtt, cmdReg, factory, log)

	assert.NotNil(t, mgr)
	assert.NotNil(t, mgr.hosts)
	assert.Equal(t, bus, mgr.bus)
	assert.Equal(t, dtt, mgr.dtt)
}

func TestManager_Add(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{}
	cmdReg := &mockCommandRegistry{}
	factory := &mockFactory{}
	log := zap.NewNop()

	mgr := NewManager(bus, dtt, cmdReg, factory, log)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "terminal1"},
		Kind: terminal.Host,
		Meta: attrs.NewBag(),
		Data: payload.New(nil),
	}

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	require.Len(t, bus.events, 2)
	_, ok := mgr.hosts[entry.ID]
	assert.True(t, ok)
}

func TestManager_Add_DecodeError(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{shouldFail: true}
	cmdReg := &mockCommandRegistry{}
	factory := &mockFactory{}
	log := zap.NewNop()

	mgr := NewManager(bus, dtt, cmdReg, factory, log)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "terminal1"},
		Kind: terminal.Host,
		Meta: attrs.NewBag(),
		Data: payload.New(nil),
	}

	err := mgr.Add(context.Background(), entry)
	require.Error(t, err)

	var termErr apierror.Error
	require.ErrorAs(t, err, &termErr)
	assert.Contains(t, termErr.Error(), "failed to decode terminal config")
}

func TestManager_Delete(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{}
	cmdReg := &mockCommandRegistry{}
	factory := &mockFactory{}
	log := zap.NewNop()

	mgr := NewManager(bus, dtt, cmdReg, factory, log)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "terminal1"},
		Kind: terminal.Host,
		Meta: attrs.NewBag(),
		Data: payload.New(nil),
	}

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	bus.events = nil

	err = mgr.Delete(context.Background(), entry)
	require.NoError(t, err)

	_, ok := mgr.hosts[entry.ID]
	assert.False(t, ok)
}

func TestManager_Delete_NotFound(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{}
	cmdReg := &mockCommandRegistry{}
	factory := &mockFactory{}
	log := zap.NewNop()

	mgr := NewManager(bus, dtt, cmdReg, factory, log)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "nonexistent"},
		Kind: terminal.Host,
	}

	err := mgr.Delete(context.Background(), entry)
	require.NoError(t, err)
}

func TestManager_Update(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{}
	cmdReg := &mockCommandRegistry{}
	factory := &mockFactory{}
	log := zap.NewNop()

	mgr := NewManager(bus, dtt, cmdReg, factory, log)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "terminal1"},
		Kind: terminal.Host,
		Meta: attrs.NewBag(),
		Data: payload.New(nil),
	}

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	bus.events = nil

	err = mgr.Update(context.Background(), entry)
	require.NoError(t, err)
}

func TestManager_GetHost(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{}
	cmdReg := &mockCommandRegistry{}
	factory := &mockFactory{}
	log := zap.NewNop()

	mgr := NewManager(bus, dtt, cmdReg, factory, log)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "terminal1"},
		Kind: terminal.Host,
		Meta: attrs.NewBag(),
		Data: payload.New(nil),
	}

	err := mgr.Add(context.Background(), entry)
	require.NoError(t, err)

	host, ok := mgr.GetHost("test:terminal1")
	assert.True(t, ok)
	assert.NotNil(t, host)

	host, ok = mgr.GetHost("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, host)
}

func TestCompositeLifecycle_OnStart(t *testing.T) {
	var globalCalled, hostCalled bool

	global := &testLifecycle{onStart: func() { globalCalled = true }}
	host := &testLifecycle{onStart: func() { hostCalled = true }}

	c := &compositeLifecycle{global: global, host: host}
	c.OnStart(context.Background(), pid.PID{}, nil)

	assert.True(t, globalCalled)
	assert.True(t, hostCalled)
}

func TestCompositeLifecycle_OnComplete(t *testing.T) {
	var globalCalled, hostCalled bool

	global := &testLifecycle{onComplete: func() { globalCalled = true }}
	host := &testLifecycle{onComplete: func() { hostCalled = true }}

	c := &compositeLifecycle{global: global, host: host}
	c.OnComplete(context.Background(), pid.PID{}, nil)

	assert.True(t, globalCalled)
	assert.True(t, hostCalled)
}

func TestCompositeLifecycle_NilHandlers(t *testing.T) {
	c := &compositeLifecycle{}

	assert.NotPanics(t, func() {
		c.OnStart(context.Background(), pid.PID{}, nil)
		c.OnComplete(context.Background(), pid.PID{}, nil)
	})
}

type testLifecycle struct {
	onStart    func()
	onComplete func()
}

func (t *testLifecycle) OnStart(context.Context, pid.PID, process.Process) error {
	if t.onStart != nil {
		t.onStart()
	}
	return nil
}

func (t *testLifecycle) OnComplete(context.Context, pid.PID, *runtime.Result) {
	if t.onComplete != nil {
		t.onComplete()
	}
}

var _ registry.EntryListener = (*Manager)(nil)
