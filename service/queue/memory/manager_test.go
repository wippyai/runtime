package memory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	memoryapi "github.com/wippyai/runtime/api/service/queue/memory"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

type mockTranscoder struct{}

func (m *mockTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
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

func TestManager_Add(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

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

func TestManager_Add_DuplicateDriver(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

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
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestManager_Add_InvalidKind(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("test", "driver"),
		Kind: "invalid.kind",
		Data: payload.New(&memoryapi.Config{}),
	}

	err := manager.Add(ctx, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported entry kind")
}

func TestManager_Update(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

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
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

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
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "driver not found")
}

func TestManager_Update_InvalidKind(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("test", "driver"),
		Kind: "invalid.kind",
		Data: payload.New(&memoryapi.Config{}),
	}

	err := manager.Update(ctx, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported entry kind")
}

func TestManager_Delete(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

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
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("test", "driver"),
		Kind: memoryapi.Kind,
		Data: payload.New(&memoryapi.Config{}),
	}

	err := manager.Delete(ctx, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "driver not found")
}

func TestManager_Delete_InvalidKind(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("test", "driver"),
		Kind: "invalid.kind",
		Data: payload.New(&memoryapi.Config{}),
	}

	err := manager.Delete(ctx, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported entry kind")
}
