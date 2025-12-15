package supervisor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	supervisorapi "github.com/wippyai/runtime/api/service/supervisor"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/internal/uniqid"
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

type mockTranscoder struct{}

func (m *mockTranscoder) Unmarshal(_ payload.Payload, out any) error {
	if cfg, ok := out.(*supervisorapi.ServiceConfig); ok {
		cfg.Process = registry.ID{NS: "test", Name: "process"}
		cfg.HostID = "test-host"
		cfg.Lifecycle = supervisor.LifecycleConfig{}
	}
	return nil
}

func (m *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

func newTestPIDGen() *uniqid.PIDGenerator {
	return uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test-node")
}

func TestManager_Add(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{}
	pidGen := newTestPIDGen()
	log := zap.NewNop()

	m := NewManager(bus, dtt, pidGen, log)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "svc1"},
		Kind: supervisorapi.ProcessService,
		Meta: attrs.NewBag(),
		Data: payload.New(nil),
	}

	err := m.Add(context.Background(), entry)
	require.NoError(t, err)

	require.Len(t, bus.events, 1)
	assert.Equal(t, supervisor.System, bus.events[0].System)
	assert.Equal(t, supervisor.ServiceRegister, bus.events[0].Kind)
	assert.Equal(t, "test:svc1", bus.events[0].Path)

	_, exists := m.services.Load(entry.ID)
	assert.True(t, exists)
}

func TestManager_Add_InvalidKind(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{}
	pidGen := newTestPIDGen()
	log := zap.NewNop()

	m := NewManager(bus, dtt, pidGen, log)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "svc1"},
		Kind: "invalid.kind",
	}

	err := m.Add(context.Background(), entry)
	require.Error(t, err)

	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "invalid entry kind", apiErr.Error())
}

func TestManager_Update(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{}
	pidGen := newTestPIDGen()
	log := zap.NewNop()

	m := NewManager(bus, dtt, pidGen, log)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "svc1"},
		Kind: supervisorapi.ProcessService,
		Meta: attrs.NewBag(),
		Data: payload.New(nil),
	}

	err := m.Add(context.Background(), entry)
	require.NoError(t, err)

	bus.events = nil // reset

	err = m.Update(context.Background(), entry)
	require.NoError(t, err)

	require.Len(t, bus.events, 1)
	assert.Equal(t, supervisor.System, bus.events[0].System)
	assert.Equal(t, supervisor.ServiceUpdate, bus.events[0].Kind)
}

func TestManager_Update_NotFound(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{}
	pidGen := newTestPIDGen()
	log := zap.NewNop()

	m := NewManager(bus, dtt, pidGen, log)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "nonexistent"},
		Kind: supervisorapi.ProcessService,
	}

	err := m.Update(context.Background(), entry)
	require.Error(t, err)

	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "service not found", apiErr.Error())
}

func TestManager_Delete(t *testing.T) {
	bus := &mockBus{}
	dtt := &mockTranscoder{}
	pidGen := newTestPIDGen()
	log := zap.NewNop()

	m := NewManager(bus, dtt, pidGen, log)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "svc1"},
		Kind: supervisorapi.ProcessService,
		Meta: attrs.NewBag(),
		Data: payload.New(nil),
	}

	err := m.Add(context.Background(), entry)
	require.NoError(t, err)

	bus.events = nil // reset

	err = m.Delete(context.Background(), entry)
	require.NoError(t, err)

	require.Len(t, bus.events, 1)
	assert.Equal(t, supervisor.System, bus.events[0].System)
	assert.Equal(t, supervisor.ServiceRemove, bus.events[0].Kind)

	_, exists := m.services.Load(entry.ID)
	assert.False(t, exists)
}

func TestManager_ImplementsEntryListener(_ *testing.T) {
	var _ registry.EntryListener = (*Manager)(nil)
}
