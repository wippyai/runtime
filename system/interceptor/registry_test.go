package interceptor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	apiinterceptor "github.com/wippyai/runtime/api/interceptor"
	"github.com/wippyai/runtime/api/runtime"
	"go.uber.org/zap"
)

type mockEventBus struct {
	events []event.Event
}

func (m *mockEventBus) Send(ctx context.Context, e event.Event) {
	m.events = append(m.events, e)
}

func (m *mockEventBus) Subscribe(ctx context.Context, system event.System, ch chan<- event.Event) (event.SubscriberID, error) {
	return "mock-sub", nil
}

func (m *mockEventBus) SubscribeP(ctx context.Context, system event.System, kind event.Kind, ch chan<- event.Event) (event.SubscriberID, error) {
	return "mock-sub", nil
}

func (m *mockEventBus) Unsubscribe(ctx context.Context, id event.SubscriberID) {
}

func (m *mockEventBus) GetEvents() []event.Event {
	return m.events
}

func setupRegistryTest() (*Registry, *mockEventBus) {
	bus := &mockEventBus{events: make([]event.Event, 0)}
	logger := zap.NewNop()
	reg := NewInterceptorRegistry(bus, logger)
	return reg, bus
}

func TestRegistry_HandleRegisterEvent(t *testing.T) {
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	interceptor := &mockInterceptor{name: "test"}

	reg.handleEvent(event.Event{
		System: apiinterceptor.System,
		Kind:   apiinterceptor.Register,
		Path:   "interceptor/test",
		Data:   interceptor,
	})

	reg.mu.RLock()
	entriesCount := len(reg.entries)
	reg.mu.RUnlock()

	assert.Equal(t, 1, entriesCount)

	events := bus.GetEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, apiinterceptor.Accept, events[0].Kind)
}

func TestRegistry_HandleRegisterEventWithEntry(t *testing.T) {
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	interceptor := &mockInterceptor{name: "test"}

	reg.handleEvent(event.Event{
		System: apiinterceptor.System,
		Kind:   apiinterceptor.Register,
		Path:   "interceptor/test",
		Data: apiinterceptor.Entry{
			Interceptor: interceptor,
			Order:       50,
		},
	})

	reg.mu.RLock()
	entriesCount := len(reg.entries)
	firstOrder := 0
	if entriesCount > 0 {
		firstOrder = reg.entries[0].order
	}
	reg.mu.RUnlock()

	assert.Equal(t, 1, entriesCount)
	assert.Equal(t, 50, firstOrder)

	events := bus.GetEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, apiinterceptor.Accept, events[0].Kind)
}

func TestRegistry_HandleRegisterEventInvalidData(t *testing.T) {
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	reg.handleEvent(event.Event{
		System: apiinterceptor.System,
		Kind:   apiinterceptor.Register,
		Path:   "interceptor/test",
		Data:   "invalid",
	})

	reg.mu.RLock()
	entriesCount := len(reg.entries)
	reg.mu.RUnlock()

	assert.Equal(t, 0, entriesCount)

	events := bus.GetEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, apiinterceptor.Reject, events[0].Kind)
}

func TestRegistry_HandleRegisterEventDuplicate(t *testing.T) {
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	interceptor := &mockInterceptor{name: "test"}

	reg.handleEvent(event.Event{
		System: apiinterceptor.System,
		Kind:   apiinterceptor.Register,
		Path:   "interceptor/test",
		Data:   interceptor,
	})

	bus.events = nil

	reg.handleEvent(event.Event{
		System: apiinterceptor.System,
		Kind:   apiinterceptor.Register,
		Path:   "interceptor/test",
		Data:   interceptor,
	})

	reg.mu.RLock()
	entriesCount := len(reg.entries)
	reg.mu.RUnlock()

	assert.Equal(t, 1, entriesCount)

	events := bus.GetEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, apiinterceptor.Reject, events[0].Kind)
}

func TestRegistry_HandleDeleteEvent(t *testing.T) {
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	interceptor := &mockInterceptor{name: "test"}

	reg.handleEvent(event.Event{
		System: apiinterceptor.System,
		Kind:   apiinterceptor.Register,
		Path:   "interceptor/test",
		Data:   interceptor,
	})

	bus.events = nil

	reg.handleEvent(event.Event{
		System: apiinterceptor.System,
		Kind:   apiinterceptor.Delete,
		Path:   "interceptor/test",
	})

	reg.mu.RLock()
	entriesCount := len(reg.entries)
	reg.mu.RUnlock()

	assert.Equal(t, 0, entriesCount)

	events := bus.GetEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, apiinterceptor.Accept, events[0].Kind)
}

func TestRegistry_HandleDeleteEventNotFound(t *testing.T) {
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	reg.handleEvent(event.Event{
		System: apiinterceptor.System,
		Kind:   apiinterceptor.Delete,
		Path:   "interceptor/nonexistent",
	})

	events := bus.GetEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, apiinterceptor.Reject, events[0].Kind)
}

func TestRegistry_OrderPreservation(t *testing.T) {
	reg, _ := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	int1 := &mockInterceptor{name: "int1"}
	int2 := &mockInterceptor{name: "int2"}
	int3 := &mockInterceptor{name: "int3"}

	reg.handleEvent(event.Event{
		System: apiinterceptor.System,
		Kind:   apiinterceptor.Register,
		Path:   "interceptor/int2",
		Data: apiinterceptor.Entry{
			Interceptor: int2,
			Order:       200,
		},
	})

	reg.handleEvent(event.Event{
		System: apiinterceptor.System,
		Kind:   apiinterceptor.Register,
		Path:   "interceptor/int1",
		Data: apiinterceptor.Entry{
			Interceptor: int1,
			Order:       100,
		},
	})

	reg.handleEvent(event.Event{
		System: apiinterceptor.System,
		Kind:   apiinterceptor.Register,
		Path:   "interceptor/int3",
		Data: apiinterceptor.Entry{
			Interceptor: int3,
			Order:       300,
		},
	})

	reg.mu.RLock()
	entries := make([]entry, len(reg.entries))
	copy(entries, reg.entries)
	reg.mu.RUnlock()

	require.Len(t, entries, 3)
	assert.Equal(t, 100, entries[0].order)
	assert.Equal(t, 200, entries[1].order)
	assert.Equal(t, 300, entries[2].order)
}

func TestRegistry_Execute(t *testing.T) {
	reg, _ := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	interceptor := &mockInterceptor{name: "test"}

	reg.handleEvent(event.Event{
		System: apiinterceptor.System,
		Kind:   apiinterceptor.Register,
		Path:   "interceptor/test",
		Data:   interceptor,
	})

	mockFunc := func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		ch := make(chan *runtime.Result, 1)
		ch <- &runtime.Result{}
		close(ch)
		return ch, nil
	}

	task := runtime.Task{}
	_, err := reg.Execute(context.Background(), mockFunc, task)

	require.NoError(t, err)
	assert.True(t, interceptor.called.Load())
}
