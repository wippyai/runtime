package processfunc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/registry"
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

func newTestPIDGen() *uniqid.PIDGenerator {
	return uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test-node")
}

func TestListener_Add_WithDefaultHost(t *testing.T) {
	bus := &mockBus{}
	pidGen := newTestPIDGen()
	log := zap.NewNop()

	l := NewListener(log, bus, pidGen)

	meta := attrs.NewBag()
	meta.Set("default_host", "test-host")

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "proc1"},
		Kind: "process.lua",
		Meta: meta,
	}

	err := l.Add(context.Background(), entry)
	require.NoError(t, err)

	require.Len(t, bus.events, 1)
	assert.Equal(t, function.System, bus.events[0].System)
	assert.Equal(t, function.Register, bus.events[0].Kind)
	assert.Equal(t, "test:proc1", bus.events[0].Path)

	l.mu.RLock()
	_, exists := l.registered["test:proc1"]
	l.mu.RUnlock()
	assert.True(t, exists)
}

func TestListener_Add_WithoutDefaultHost(t *testing.T) {
	bus := &mockBus{}
	pidGen := newTestPIDGen()
	log := zap.NewNop()

	l := NewListener(log, bus, pidGen)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "proc1"},
		Kind: "process.lua",
		Meta: attrs.NewBag(),
	}

	err := l.Add(context.Background(), entry)
	require.NoError(t, err)

	assert.Len(t, bus.events, 0)

	l.mu.RLock()
	_, exists := l.registered["test:proc1"]
	l.mu.RUnlock()
	assert.False(t, exists)
}

func TestListener_Add_NonProcessKind(t *testing.T) {
	bus := &mockBus{}
	pidGen := newTestPIDGen()
	log := zap.NewNop()

	l := NewListener(log, bus, pidGen)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "svc1"},
		Kind: "service.http",
		Meta: attrs.NewBag(),
	}

	err := l.Add(context.Background(), entry)
	require.NoError(t, err)
	assert.Len(t, bus.events, 0)
}

func TestListener_Update_HostChange(t *testing.T) {
	bus := &mockBus{}
	pidGen := newTestPIDGen()
	log := zap.NewNop()

	l := NewListener(log, bus, pidGen)

	meta := attrs.NewBag()
	meta.Set("default_host", "host1")

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "proc1"},
		Kind: "process.lua",
		Meta: meta,
	}

	err := l.Add(context.Background(), entry)
	require.NoError(t, err)

	bus.events = nil

	meta2 := attrs.NewBag()
	meta2.Set("default_host", "host2")
	entry.Meta = meta2

	err = l.Update(context.Background(), entry)
	require.NoError(t, err)

	require.Len(t, bus.events, 2)
	assert.Equal(t, function.Delete, bus.events[0].Kind)
	assert.Equal(t, function.Register, bus.events[1].Kind)
}

func TestListener_Delete(t *testing.T) {
	bus := &mockBus{}
	pidGen := newTestPIDGen()
	log := zap.NewNop()

	l := NewListener(log, bus, pidGen)

	meta := attrs.NewBag()
	meta.Set("default_host", "test-host")

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "proc1"},
		Kind: "process.lua",
		Meta: meta,
	}

	err := l.Add(context.Background(), entry)
	require.NoError(t, err)

	bus.events = nil

	err = l.Delete(context.Background(), entry)
	require.NoError(t, err)

	require.Len(t, bus.events, 1)
	assert.Equal(t, function.System, bus.events[0].System)
	assert.Equal(t, function.Delete, bus.events[0].Kind)

	l.mu.RLock()
	_, exists := l.registered["test:proc1"]
	l.mu.RUnlock()
	assert.False(t, exists)
}

func TestListener_OptionsFromMeta(t *testing.T) {
	bus := &mockBus{}
	pidGen := newTestPIDGen()
	log := zap.NewNop()

	l := NewListener(log, bus, pidGen)

	options := attrs.NewBag()
	options.Set("default_host", "test-host")
	options.Set("timeout", "30s")

	meta := attrs.NewBag()
	meta.Set("options", options)

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "proc1"},
		Kind: "process.lua",
		Meta: meta,
	}

	err := l.Add(context.Background(), entry)
	require.NoError(t, err)

	require.Len(t, bus.events, 1)
	funcEntry, ok := bus.events[0].Data.(*function.FuncEntry)
	require.True(t, ok)
	assert.NotNil(t, funcEntry.Options)
	assert.Equal(t, "30s", funcEntry.Options.GetString("timeout", ""))
}

func TestListener_ImplementsEntryListener(_ *testing.T) {
	var _ registry.EntryListener = (*Listener)(nil)
}
