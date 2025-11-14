package interceptor

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	apiinterceptor "github.com/wippyai/runtime/api/interceptor"
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

func (m *mockEventBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {
}

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

func setupRegistryTest() (*Registry, *mockEventBus) {
	bus := &mockEventBus{}
	logger := zap.NewNop()
	reg := NewInterceptorRegistry(bus, logger)
	return reg, bus
}

func TestRegistry_StartStop(t *testing.T) {
	reg, _ := setupRegistryTest()

	err := reg.Start(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, reg.subscriber)

	err = reg.Stop()
	require.NoError(t, err)
}

func TestRegistry_RegisterInterceptor(t *testing.T) {
	reg, _ := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	interceptor := &mockInterceptor{name: "test"}

	err := reg.Register("test-interceptor", interceptor)
	require.NoError(t, err)

	assert.Len(t, reg.interceptors, 1)
	assert.Equal(t, interceptor, reg.interceptors[0])
}

func TestRegistry_RegisterDuplicate(t *testing.T) {
	reg, _ := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	interceptor := &mockInterceptor{name: "test"}

	err := reg.Register("test-interceptor", interceptor)
	require.NoError(t, err)

	err = reg.Register("test-interceptor-2", interceptor)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestRegistry_HandleRegisterEvent(t *testing.T) {
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	interceptor := &mockInterceptor{name: "test"}

	reg.handleEvent(event.Event{
		Kind: apiinterceptor.Register,
		Path: "interceptor/test",
		Data: interceptor,
	})

	assert.Len(t, reg.interceptors, 1)

	events := bus.GetEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, apiinterceptor.Accept, events[0].Kind)
}

func TestRegistry_HandleRegisterEventInvalidData(t *testing.T) {
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	reg.handleEvent(event.Event{
		Kind: apiinterceptor.Register,
		Path: "interceptor/test",
		Data: "invalid data",
	})

	assert.Len(t, reg.interceptors, 0)

	events := bus.GetEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, apiinterceptor.Reject, events[0].Kind)
}

func TestRegistry_HandleUpdateEvent(t *testing.T) {
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	interceptor := &mockInterceptor{name: "test"}
	reg.interceptors = append(reg.interceptors, interceptor)

	reg.handleEvent(event.Event{
		Kind: apiinterceptor.Update,
		Path: "interceptor/test",
		Data: interceptor,
	})

	events := bus.GetEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, apiinterceptor.Accept, events[0].Kind)
}

func TestRegistry_HandleUpdateEventNotFound(t *testing.T) {
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	interceptor := &mockInterceptor{name: "test"}

	reg.handleEvent(event.Event{
		Kind: apiinterceptor.Update,
		Path: "interceptor/test",
		Data: interceptor,
	})

	events := bus.GetEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, apiinterceptor.Reject, events[0].Kind)
}

func TestRegistry_HandleDeleteEvent(t *testing.T) {
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	interceptor := &mockInterceptor{name: "test"}
	reg.interceptors = append(reg.interceptors, interceptor)

	reg.handleEvent(event.Event{
		Kind: apiinterceptor.Delete,
		Path: "interceptor/test",
		Data: interceptor,
	})

	assert.Len(t, reg.interceptors, 0)

	events := bus.GetEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, apiinterceptor.Accept, events[0].Kind)
}

func TestRegistry_ExecuteImplementsChain(t *testing.T) {
	reg, _ := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	int1 := &mockInterceptor{name: "int1"}
	int2 := &mockInterceptor{name: "int2"}

	reg.interceptors = []apiinterceptor.Interceptor{int1, int2}

	executed := false
	mockFunc := func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		executed = true
		ch := make(chan *runtime.Result, 1)
		ch <- &runtime.Result{}
		return ch, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.ID{NS: "test", Name: "func"}}

	ch, err := reg.Execute(ctx, mockFunc, task)
	require.NoError(t, err)
	require.NotNil(t, ch)

	result := <-ch
	assert.NotNil(t, result)
	assert.True(t, int1.called)
	assert.True(t, int2.called)
	assert.True(t, executed)
}

func TestRegistry_ConcurrentRegistration(t *testing.T) {
	reg, _ := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	var wg sync.WaitGroup
	numGoroutines := 10

	// Use the same interceptor instance to test duplicate detection
	sharedInterceptor := &mockInterceptor{name: "shared"}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			reg.handleEvent(event.Event{
				Kind: apiinterceptor.Register,
				Path: "interceptor/test",
				Data: sharedInterceptor,
			})
		}(i)
	}

	wg.Wait()

	// Only first registration of the same interceptor instance should succeed
	assert.Len(t, reg.interceptors, 1)
	assert.Equal(t, sharedInterceptor, reg.interceptors[0])
}

func TestRegistry_ConcurrentExecute(t *testing.T) {
	reg, _ := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	int1 := &mockInterceptor{name: "int1"}
	reg.interceptors = []apiinterceptor.Interceptor{int1}

	mockFunc := func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		ch := make(chan *runtime.Result, 1)
		ch <- &runtime.Result{}
		return ch, nil
	}

	ctx := context.Background()
	task := runtime.Task{ID: registry.ID{NS: "test", Name: "func"}}

	var wg sync.WaitGroup
	numGoroutines := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, err := reg.Execute(ctx, mockFunc, task)
			require.NoError(t, err)
			<-ch
		}()
	}

	wg.Wait()
}

func TestRegistry_Unregister(t *testing.T) {
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	interceptor := &mockInterceptor{name: "test"}
	err := reg.Register("test-interceptor", interceptor)
	require.NoError(t, err)
	assert.Len(t, reg.interceptors, 1)

	err = reg.Unregister("test-interceptor")
	require.NoError(t, err)
	assert.Len(t, reg.interceptors, 0)

	events := bus.GetEvents()
	found := false
	for _, e := range events {
		if e.Kind == apiinterceptor.Delete {
			found = true
			break
		}
	}
	assert.True(t, found, "Delete event should be sent")
}

func TestRegistry_UnregisterNonExistent(t *testing.T) {
	reg, _ := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	err := reg.Unregister("non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRegistry_Get(t *testing.T) {
	reg, _ := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	interceptor := &mockInterceptor{name: "test"}
	err := reg.Register("test-interceptor", interceptor)
	require.NoError(t, err)

	retrieved, err := reg.Get("test-interceptor")
	require.NoError(t, err)
	assert.Equal(t, interceptor, retrieved)
}

func TestRegistry_GetNonExistent(t *testing.T) {
	reg, _ := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	retrieved, err := reg.Get("non-existent")
	assert.Error(t, err)
	assert.Nil(t, retrieved)
	assert.Contains(t, err.Error(), "not found")
}

func TestRegistry_List(t *testing.T) {
	reg, _ := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	int1 := &mockInterceptor{name: "int1"}
	int2 := &mockInterceptor{name: "int2"}
	int3 := &mockInterceptor{name: "int3"}

	reg.Register("interceptor1", int1)
	reg.Register("interceptor2", int2)
	reg.Register("interceptor3", int3)

	list := reg.List()
	assert.Len(t, list, 3)
	assert.Contains(t, list, "interceptor1")
	assert.Contains(t, list, "interceptor2")
	assert.Contains(t, list, "interceptor3")
}

func TestRegistry_ListEmpty(t *testing.T) {
	reg, _ := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	list := reg.List()
	assert.Len(t, list, 0)
}

func TestNopInterceptor(t *testing.T) {
	nop := NewNopInterceptor()
	assert.NotNil(t, nop)

	ctx := context.Background()
	called := false
	next := func(ctx context.Context) (*runtime.Result, context.Context) {
		called = true
		return &runtime.Result{}, ctx
	}

	result, returnedCtx := nop.Handle(ctx, next)
	assert.True(t, called, "next function should be called")
	assert.NotNil(t, result)
	assert.Equal(t, ctx, returnedCtx)
}

func TestRegistry_UpdateInterceptorNotFound(t *testing.T) {
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	interceptor := &mockInterceptor{name: "test"}

	reg.handleEvent(event.Event{
		Kind: apiinterceptor.Update,
		Path: "interceptor/test",
		Data: interceptor,
	})

	events := bus.GetEvents()
	found := false
	for _, e := range events {
		if e.Kind == apiinterceptor.Reject {
			found = true
			break
		}
	}
	assert.True(t, found, "Reject event should be sent when updating non-existent interceptor")
}

func TestRegistry_DeleteInterceptorNotFound(t *testing.T) {
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	reg.handleEvent(event.Event{
		Kind: apiinterceptor.Delete,
		Path: "interceptor/test",
	})

	events := bus.GetEvents()
	found := false
	for _, e := range events {
		if e.Kind == apiinterceptor.Reject {
			found = true
			break
		}
	}
	assert.True(t, found, "Reject event should be sent when deleting non-existent interceptor")
}

func TestRegistry_HandleUpdateEventInvalidData(t *testing.T) {
	reg, bus := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	reg.handleEvent(event.Event{
		Kind: apiinterceptor.Update,
		Path: "interceptor/test",
		Data: "invalid data",
	})

	events := bus.GetEvents()
	found := false
	for _, e := range events {
		if e.Kind == apiinterceptor.Reject {
			found = true
			break
		}
	}
	assert.True(t, found, "Reject event should be sent for invalid data type")
}

func TestRegistry_HandleUnknownEvent(t *testing.T) {
	reg, _ := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	reg.handleEvent(event.Event{
		Kind: "unknown.event",
		Path: "interceptor/test",
	})
}

func TestRegistry_RegisterByNameAlreadyExists(t *testing.T) {
	reg, _ := setupRegistryTest()
	require.NoError(t, reg.Start(context.Background()))
	defer reg.Stop()

	interceptor := &mockInterceptor{name: "test"}
	err := reg.Register("test-interceptor", interceptor)
	require.NoError(t, err)

	err = reg.Register("test-interceptor", &mockInterceptor{name: "other"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}
