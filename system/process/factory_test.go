package process

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

type mockProcess struct {
	closed bool
}

func (p *mockProcess) Init(_ context.Context, _ string, _ payload.Payloads) error { return nil }
func (p *mockProcess) Step(_ []process.Event, out *process.StepOutput) error {
	out.Done(nil)
	return nil
}
func (p *mockProcess) Close() { p.closed = true }

func newMockFactory() process.FactoryFunc {
	return func() (process.Process, error) {
		return &mockProcess{}, nil
	}
}

func newErrorFactory(err error) process.FactoryFunc {
	return func() (process.Process, error) {
		return nil, err
	}
}

func setupFactoryRegistryTest() (*FactoryRegistry, event.Bus) {
	bus := eventbus.NewBus()
	reg := NewFactoryRegistry(bus, zap.NewNop())
	return reg, bus
}

func TestFactoryRegistry_StartStop(t *testing.T) {
	reg, _ := setupFactoryRegistryTest()
	ctx := context.Background()

	err := reg.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, reg.subscriber)
	assert.Equal(t, ctx, reg.ctx)

	err = reg.Stop()
	require.NoError(t, err)
}

func TestFactoryRegistry_RegisterFactory(t *testing.T) {
	reg, bus := setupFactoryRegistryTest()
	ctx := context.Background()
	require.NoError(t, reg.Start(ctx))
	defer func() { _ = reg.Stop() }()

	responses := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(ctx, bus, process.System, "factory.(accept|reject)", func(e event.Event) {
		responses <- e
	})
	require.NoError(t, err)
	defer sub.Close()

	factoryID := "test:factory1"
	bus.Send(ctx, event.Event{
		System: process.System,
		Kind:   process.FactoryRegister,
		Path:   factoryID,
		Data: &process.FactoryEntry{
			Factory: newMockFactory(),
			Meta:    process.Meta{Method: "handle"},
		},
	})

	select {
	case resp := <-responses:
		assert.Equal(t, process.FactoryAccept, resp.Kind)
		assert.Equal(t, factoryID, resp.Path)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}

	id := registry.ParseID(factoryID)
	assert.True(t, reg.Has(id))
}

func TestFactoryRegistry_RegisterInvalidPayload(t *testing.T) {
	reg, bus := setupFactoryRegistryTest()
	ctx := context.Background()
	require.NoError(t, reg.Start(ctx))
	defer func() { _ = reg.Stop() }()

	responses := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(ctx, bus, process.System, "factory.(accept|reject)", func(e event.Event) {
		responses <- e
	})
	require.NoError(t, err)
	defer sub.Close()

	factoryID := "test:invalid"
	bus.Send(ctx, event.Event{
		System: process.System,
		Kind:   process.FactoryRegister,
		Path:   factoryID,
		Data:   "invalid payload",
	})

	select {
	case resp := <-responses:
		assert.Equal(t, process.FactoryReject, resp.Kind)
		assert.Equal(t, factoryID, resp.Path)
		assert.Contains(t, resp.Data.(string), "invalid factory entry")
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}

	id := registry.ParseID(factoryID)
	assert.False(t, reg.Has(id))
}

func TestFactoryRegistry_DeleteFactory(t *testing.T) {
	reg, bus := setupFactoryRegistryTest()
	ctx := context.Background()
	require.NoError(t, reg.Start(ctx))
	defer func() { _ = reg.Stop() }()

	responses := make(chan event.Event, 2)
	sub, err := eventbus.NewSubscriber(ctx, bus, process.System, "factory.(accept|reject)", func(e event.Event) {
		responses <- e
	})
	require.NoError(t, err)
	defer sub.Close()

	factoryID := "test:todelete"
	bus.Send(ctx, event.Event{
		System: process.System,
		Kind:   process.FactoryRegister,
		Path:   factoryID,
		Data: &process.FactoryEntry{
			Factory: newMockFactory(),
			Meta:    process.Meta{Method: "handle"},
		},
	})

	select {
	case <-responses:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for register response")
	}

	id := registry.ParseID(factoryID)
	assert.True(t, reg.Has(id))

	bus.Send(ctx, event.Event{
		System: process.System,
		Kind:   process.FactoryDelete,
		Path:   factoryID,
	})

	select {
	case resp := <-responses:
		assert.Equal(t, process.FactoryAccept, resp.Kind)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for delete response")
	}

	assert.False(t, reg.Has(id))
}

func TestFactoryRegistry_DeleteNotFound(t *testing.T) {
	reg, bus := setupFactoryRegistryTest()
	ctx := context.Background()
	require.NoError(t, reg.Start(ctx))
	defer func() { _ = reg.Stop() }()

	responses := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(ctx, bus, process.System, "factory.(accept|reject)", func(e event.Event) {
		responses <- e
	})
	require.NoError(t, err)
	defer sub.Close()

	bus.Send(ctx, event.Event{
		System: process.System,
		Kind:   process.FactoryDelete,
		Path:   "test:nonexistent",
	})

	select {
	case resp := <-responses:
		assert.Equal(t, process.FactoryReject, resp.Kind)
		assert.Contains(t, resp.Data.(string), "factory not found")
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}
}

func TestFactoryRegistry_Create(t *testing.T) {
	reg, _ := setupFactoryRegistryTest()
	ctx := context.Background()
	require.NoError(t, reg.Start(ctx))
	defer func() { _ = reg.Stop() }()

	id := registry.NewID("test", "factory")
	reg.factories.Store(id, &factoryEntry{
		factory: newMockFactory(),
		meta:    process.Meta{Method: "handle"},
	})

	proc, meta, err := reg.Create(id)
	require.NoError(t, err)
	assert.NotNil(t, proc)
	assert.NotNil(t, meta)
	assert.Equal(t, "handle", meta.Method)
}

func TestFactoryRegistry_CreateNotFound(t *testing.T) {
	reg, _ := setupFactoryRegistryTest()
	ctx := context.Background()
	require.NoError(t, reg.Start(ctx))
	defer func() { _ = reg.Stop() }()

	id := registry.NewID("test", "nonexistent")
	proc, meta, err := reg.Create(id)
	assert.Error(t, err)
	assert.Nil(t, proc)
	assert.Nil(t, meta)
	assert.Contains(t, err.Error(), "no factory registered")
}

func TestFactoryRegistry_CreateFactoryError(t *testing.T) {
	reg, _ := setupFactoryRegistryTest()
	ctx := context.Background()
	require.NoError(t, reg.Start(ctx))
	defer func() { _ = reg.Stop() }()

	id := registry.NewID("test", "errorfactory")
	factoryErr := errors.New("factory creation failed")
	reg.factories.Store(id, &factoryEntry{
		factory: newErrorFactory(factoryErr),
		meta:    process.Meta{Method: "handle"},
	})

	proc, meta, err := reg.Create(id)
	assert.Error(t, err)
	assert.Nil(t, proc)
	assert.Nil(t, meta)
	assert.Contains(t, err.Error(), "failed to create process")
}

func TestFactoryRegistry_Has(t *testing.T) {
	reg, _ := setupFactoryRegistryTest()
	ctx := context.Background()
	require.NoError(t, reg.Start(ctx))
	defer func() { _ = reg.Stop() }()

	id := registry.NewID("test", "exists")
	assert.False(t, reg.Has(id))

	reg.factories.Store(id, &factoryEntry{
		factory: newMockFactory(),
		meta:    process.Meta{},
	})
	assert.True(t, reg.Has(id))
}

func TestFactoryRegistry_ConcurrentAccess(t *testing.T) {
	reg, bus := setupFactoryRegistryTest()
	ctx := context.Background()
	require.NoError(t, reg.Start(ctx))
	defer func() { _ = reg.Stop() }()

	const numFactories = 10
	var wg sync.WaitGroup

	for i := 0; i < numFactories; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			bus.Send(ctx, event.Event{
				System: process.System,
				Kind:   process.FactoryRegister,
				Path:   fmt.Sprintf("test:factory-%d", idx),
				Data: &process.FactoryEntry{
					Factory: newMockFactory(),
					Meta:    process.Meta{Method: fmt.Sprintf("handle-%d", idx)},
				},
			})
		}(i)
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	for i := 0; i < numFactories; i++ {
		id := registry.NewID("test", fmt.Sprintf("factory-%d", i))
		assert.True(t, reg.Has(id), "factory %d should exist", i)
	}
}

func TestFactoryRegistry_HandleEventUnknownKind(t *testing.T) {
	reg, _ := setupFactoryRegistryTest()
	ctx := context.Background()
	require.NoError(t, reg.Start(ctx))
	defer func() { _ = reg.Stop() }()

	// Call handleEvent directly with unknown kind to test defensive code
	reg.handleEvent(event.Event{
		System: process.System,
		Kind:   "factory.unknown",
		Path:   "test:unknown",
	})

	// Should not have registered anything
	id := registry.ParseID("test:unknown")
	assert.False(t, reg.Has(id))
}

func TestFactoryRegistry_CreateInvalidEntryType(t *testing.T) {
	reg, _ := setupFactoryRegistryTest()
	ctx := context.Background()
	require.NoError(t, reg.Start(ctx))
	defer func() { _ = reg.Stop() }()

	// Store invalid entry type directly
	id := registry.NewID("test", "invalid-type")
	reg.factories.Store(id, "not-a-factory-entry")

	proc, meta, err := reg.Create(id)
	assert.Error(t, err)
	assert.Nil(t, proc)
	assert.Nil(t, meta)
	assert.Contains(t, err.Error(), "invalid factory entry")
}
