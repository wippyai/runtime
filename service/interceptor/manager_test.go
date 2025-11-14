package interceptor

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	apiinterceptor "github.com/wippyai/runtime/api/interceptor"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"go.uber.org/zap"
)

type mockEventBus struct {
	events []event.Event
	mu     sync.Mutex
}

func (m *mockEventBus) Publish(e event.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
	return nil
}

func (m *mockEventBus) Subscribe(_ context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockEventBus) SubscribeP(_ context.Context, _ event.System, _ event.Kind, _ chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}

func (m *mockEventBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {}

func (m *mockEventBus) Send(_ context.Context, e event.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
}

func (m *mockEventBus) GetEvents() []event.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]event.Event{}, m.events...)
}

type mockInterceptor struct {
	name string
}

func (m *mockInterceptor) Handle(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context) {
	return next(ctx)
}

func TestManager_Add(t *testing.T) {
	bus := &mockEventBus{}
	manager := NewManager(bus, zap.NewNop())

	interceptor := &mockInterceptor{name: "test"}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "interceptor"},
		Data: payload.New(interceptor),
	}

	err := manager.Add(context.Background(), entry)
	require.NoError(t, err)

	events := bus.GetEvents()
	require.Len(t, events, 1)
	assert.Equal(t, apiinterceptor.System, events[0].System)
	assert.Equal(t, apiinterceptor.Register, events[0].Kind)
	assert.Equal(t, "test:interceptor", events[0].Path)
	assert.Equal(t, interceptor, events[0].Data)
}

func TestManager_AddInvalidType(t *testing.T) {
	bus := &mockEventBus{}
	manager := NewManager(bus, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "interceptor"},
		Data: payload.New("invalid"),
	}

	err := manager.Add(context.Background(), entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid interceptor data type")

	events := bus.GetEvents()
	assert.Len(t, events, 0)
}

func TestManager_Update(t *testing.T) {
	bus := &mockEventBus{}
	manager := NewManager(bus, zap.NewNop())

	interceptor := &mockInterceptor{name: "test"}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "interceptor"},
		Data: payload.New(interceptor),
	}

	err := manager.Update(context.Background(), entry)
	require.NoError(t, err)

	events := bus.GetEvents()
	require.Len(t, events, 1)
	assert.Equal(t, apiinterceptor.System, events[0].System)
	assert.Equal(t, apiinterceptor.Update, events[0].Kind)
	assert.Equal(t, "test:interceptor", events[0].Path)
}

func TestManager_UpdateInvalidType(t *testing.T) {
	bus := &mockEventBus{}
	manager := NewManager(bus, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "interceptor"},
		Data: payload.New("invalid"),
	}

	err := manager.Update(context.Background(), entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid interceptor data type")

	events := bus.GetEvents()
	assert.Len(t, events, 0)
}

func TestManager_Delete(t *testing.T) {
	bus := &mockEventBus{}
	manager := NewManager(bus, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "interceptor"},
		Data: payload.New(&mockInterceptor{name: "test"}),
	}

	err := manager.Delete(context.Background(), entry)
	require.NoError(t, err)

	events := bus.GetEvents()
	require.Len(t, events, 1)
	assert.Equal(t, apiinterceptor.System, events[0].System)
	assert.Equal(t, apiinterceptor.Delete, events[0].Kind)
	assert.Equal(t, "test:interceptor", events[0].Path)
}
