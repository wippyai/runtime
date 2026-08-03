// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"context"
	"errors"
	"sync"
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

type mockFactory struct {
	meta *process.Meta
	proc process.Process
}

func (m *mockFactory) Create(_ registry.ID) (process.Process, *process.Meta, error) {
	if m.proc != nil {
		return m.proc, m.meta, nil
	}
	return &mockProcess{}, m.meta, nil
}

type mockProcess struct {
	initContext context.Context
	mu          sync.Mutex
}

func (m *mockProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initContext = ctx
	return nil
}

func (m *mockProcess) context() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.initContext
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
	assert.Contains(t, termErr.Details().GetString("cause", ""), assert.AnError.Error())
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
	_ = c.OnStart(context.Background(), pid.PID{}, nil)

	assert.True(t, globalCalled)
	assert.True(t, hostCalled)
}

func TestCompositeLifecycle_OnStart_HostErrorRollsBackGlobal(t *testing.T) {
	hostErr := errors.New("host lifecycle error")
	var rollbackResult *runtime.Result

	global := &testLifecycle{onComplete: func(result *runtime.Result) { rollbackResult = result }}
	host := &testLifecycle{onStartErr: hostErr}

	c := &compositeLifecycle{global: global, host: host}
	err := c.OnStart(context.Background(), pid.PID{}, nil)

	assert.ErrorIs(t, err, hostErr)
	require.NotNil(t, rollbackResult)
	assert.ErrorIs(t, rollbackResult.Error, hostErr)
}

func TestCompositeLifecycle_OnComplete(t *testing.T) {
	var globalCalled, hostCalled bool

	global := &testLifecycle{onComplete: func(*runtime.Result) { globalCalled = true }}
	host := &testLifecycle{onComplete: func(*runtime.Result) { hostCalled = true }}

	c := &compositeLifecycle{global: global, host: host}
	c.OnComplete(context.Background(), pid.PID{}, nil)

	assert.True(t, globalCalled)
	assert.True(t, hostCalled)
}

func TestCompositeLifecycle_NilHandlers(t *testing.T) {
	c := &compositeLifecycle{}

	assert.NotPanics(t, func() {
		_ = c.OnStart(context.Background(), pid.PID{}, nil)
		c.OnComplete(context.Background(), pid.PID{}, nil)
	})
}

type testLifecycle struct {
	onStart    func()
	onStartErr error
	onComplete func(*runtime.Result)
}

func (t *testLifecycle) OnStart(context.Context, pid.PID, process.Process) error {
	if t.onStart != nil {
		t.onStart()
	}
	return t.onStartErr
}

func (t *testLifecycle) OnComplete(_ context.Context, _ pid.PID, result *runtime.Result) {
	if t.onComplete != nil {
		t.onComplete(result)
	}
}

var _ registry.EntryListener = (*Manager)(nil)
