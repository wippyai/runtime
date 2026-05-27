// SPDX-License-Identifier: MPL-2.0

package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	memoryapi "github.com/wippyai/runtime/api/service/queue/memory"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

type mockTranscoder struct{}

func (m *mockTranscoder) Unmarshal(p payload.Payload, v any) error {
	if cfg, ok := v.(*memoryapi.Config); ok {
		if src, ok := p.Data().(*memoryapi.Config); ok {
			*cfg = *src
			return nil
		}
	}
	return nil
}

func (m *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

func newManagerTestContext(t *testing.T, bus event.Bus) context.Context {
	t.Helper()

	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})

	awaitSvc := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitSvc.Start(ctx))
	t.Cleanup(func() { _ = awaitSvc.Stop() })
	ctx = event.WithAwaitService(ctx, awaitSvc)

	sub, err := eventbus.NewSubscriber(ctx, bus, queueapi.System,
		"queue.driver.(register|delete)", func(e event.Event) {
			bus.Send(ctx, event.Event{
				System: queueapi.System,
				Kind:   "queue.accept",
				Path:   e.Path,
			})
		})
	require.NoError(t, err)
	t.Cleanup(sub.Close)

	return ctx
}

func TestManager_Add(t *testing.T) {
	bus := eventbus.NewBus()
	ctx := newManagerTestContext(t, bus)

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	config := &memoryapi.Config{
		Lifecycle: supervisor.LifecycleConfig{
			AutoStart: true,
		},
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "driver"),
		Kind: memoryapi.Kind,
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	driver, exists := manager.drivers[entry.ID]
	assert.True(t, exists)
	assert.NotNil(t, driver)
	assert.Equal(t, entry.ID, driver.id)
}

func TestManager_Add_WaitsForDriverRegisterAck(t *testing.T) {
	bus := eventbus.NewBus()
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})

	awaitSvc := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitSvc.Start(ctx))
	t.Cleanup(func() { _ = awaitSvc.Stop() })
	ctx = event.WithAwaitService(ctx, awaitSvc)

	seenRegister := make(chan struct{})
	releaseAccept := make(chan struct{})
	sub, err := eventbus.NewSubscriber(ctx, bus, queueapi.System,
		"queue.driver.register", func(e event.Event) {
			close(seenRegister)
			<-releaseAccept
			bus.Send(ctx, event.Event{
				System: queueapi.System,
				Kind:   "queue.accept",
				Path:   e.Path,
			})
		})
	require.NoError(t, err)
	t.Cleanup(sub.Close)

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())
	entry := registry.Entry{
		ID:   registry.NewID("test", "driver"),
		Kind: memoryapi.Kind,
		Data: payload.New(&memoryapi.Config{}),
	}

	done := make(chan error, 1)
	go func() { done <- manager.Add(ctx, entry) }()

	select {
	case <-seenRegister:
	case <-time.After(time.Second):
		t.Fatal("driver register event was not emitted")
	}

	select {
	case err := <-done:
		t.Fatalf("Add returned before queue manager ack: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseAccept)
	require.NoError(t, <-done)
}

func TestManager_Add_DuplicateDriver(t *testing.T) {
	bus := eventbus.NewBus()
	ctx := newManagerTestContext(t, bus)

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	config := &memoryapi.Config{
		Lifecycle: supervisor.LifecycleConfig{
			AutoStart: true,
		},
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "driver"),
		Kind: memoryapi.Kind,
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	err = manager.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestManager_Add_InvalidKind(t *testing.T) {
	bus := eventbus.NewBus()
	ctx := newManagerTestContext(t, bus)

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("test", "driver"),
		Kind: "invalid.kind",
		Data: payload.New(&memoryapi.Config{}),
	}

	err := manager.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported entry kind")
}

func TestManager_Update(t *testing.T) {
	bus := eventbus.NewBus()
	ctx := newManagerTestContext(t, bus)

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	config := &memoryapi.Config{
		Lifecycle: supervisor.LifecycleConfig{
			AutoStart: true,
		},
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "driver"),
		Kind: memoryapi.Kind,
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	newConfig := &memoryapi.Config{
		Lifecycle: supervisor.LifecycleConfig{
			AutoStart: false,
		},
	}

	updatedEntry := registry.Entry{
		ID:   registry.NewID("test", "driver"),
		Kind: memoryapi.Kind,
		Data: payload.New(newConfig),
	}

	err = manager.Update(ctx, updatedEntry)
	require.NoError(t, err)

	driver, exists := manager.drivers[entry.ID]
	assert.True(t, exists)
	assert.NotNil(t, driver)
}

func TestManager_Update_DriverNotFound(t *testing.T) {
	bus := eventbus.NewBus()
	ctx := newManagerTestContext(t, bus)

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	config := &memoryapi.Config{
		Lifecycle: supervisor.LifecycleConfig{
			AutoStart: true,
		},
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "driver"),
		Kind: memoryapi.Kind,
		Data: payload.New(config),
	}

	err := manager.Update(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "driver not found")
}

func TestManager_Update_InvalidKind(t *testing.T) {
	bus := eventbus.NewBus()
	ctx := newManagerTestContext(t, bus)

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("test", "driver"),
		Kind: "invalid.kind",
		Data: payload.New(&memoryapi.Config{}),
	}

	err := manager.Update(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported entry kind")
}

func TestManager_Delete(t *testing.T) {
	bus := eventbus.NewBus()
	ctx := newManagerTestContext(t, bus)

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	config := &memoryapi.Config{
		Lifecycle: supervisor.LifecycleConfig{
			AutoStart: true,
		},
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "driver"),
		Kind: memoryapi.Kind,
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	err = manager.Delete(ctx, entry)
	require.NoError(t, err)

	_, exists := manager.drivers[entry.ID]
	assert.False(t, exists)
}

func TestManager_Delete_DriverNotFound(t *testing.T) {
	bus := eventbus.NewBus()
	ctx := newManagerTestContext(t, bus)

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("test", "driver"),
		Kind: memoryapi.Kind,
		Data: payload.New(&memoryapi.Config{}),
	}

	err := manager.Delete(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "driver not found")
}

func TestManager_Delete_InvalidKind(t *testing.T) {
	bus := eventbus.NewBus()
	ctx := newManagerTestContext(t, bus)

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("test", "driver"),
		Kind: "invalid.kind",
		Data: payload.New(&memoryapi.Config{}),
	}

	err := manager.Delete(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported entry kind")
}
