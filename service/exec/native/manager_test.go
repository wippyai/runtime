package native

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

type mockTranscoder struct{}

func (m *mockTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
	if cfg, ok := v.(*exec.NativeExecutorConfig); ok {
		if src, ok := p.Data().(*exec.NativeExecutorConfig); ok {
			*cfg = *src
			return nil
		}
	}
	return nil
}

func (m *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

type mockFactory struct {
	createErr error
}

func (f *mockFactory) CreateExecutor(_ registry.ID, cfg *exec.NativeExecutorConfig) (exec.ProcessExecutor, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	return NewNativeExecutor(zap.NewNop(), cfg), nil
}

func TestManager_Add(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	config := &exec.NativeExecutorConfig{
		DefaultWorkDir: "/tmp",
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "executor"),
		Kind: exec.KindNativeExecutor,
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	provider, exists := manager.executors[entry.ID]
	assert.True(t, exists)
	assert.NotNil(t, provider)
}

func TestManager_Add_DuplicateExecutor(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	config := &exec.NativeExecutorConfig{
		DefaultWorkDir: "/tmp",
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "executor"),
		Kind: exec.KindNativeExecutor,
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
		ID:   registry.NewID("test", "executor"),
		Kind: "invalid.kind",
		Data: payload.New(&exec.NativeExecutorConfig{}),
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

	config := &exec.NativeExecutorConfig{
		DefaultWorkDir: "/tmp",
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "executor"),
		Kind: exec.KindNativeExecutor,
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	newConfig := &exec.NativeExecutorConfig{
		DefaultWorkDir: "/var/tmp",
	}

	updatedEntry := registry.Entry{
		ID:   registry.NewID("test", "executor"),
		Kind: exec.KindNativeExecutor,
		Data: payload.New(newConfig),
	}

	err = manager.Update(ctx, updatedEntry)
	require.NoError(t, err)

	provider, exists := manager.executors[entry.ID]
	assert.True(t, exists)
	assert.NotNil(t, provider)
}

func TestManager_Update_ExecutorNotFound(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	config := &exec.NativeExecutorConfig{
		DefaultWorkDir: "/tmp",
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "executor"),
		Kind: exec.KindNativeExecutor,
		Data: payload.New(config),
	}

	err := manager.Update(ctx, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_Update_InvalidKind(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("test", "executor"),
		Kind: "invalid.kind",
		Data: payload.New(&exec.NativeExecutorConfig{}),
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

	config := &exec.NativeExecutorConfig{
		DefaultWorkDir: "/tmp",
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "executor"),
		Kind: exec.KindNativeExecutor,
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	err = manager.Delete(ctx, entry)
	require.NoError(t, err)

	_, exists := manager.executors[entry.ID]
	assert.False(t, exists)
}

func TestManager_Delete_ExecutorNotFound(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("test", "executor"),
		Kind: exec.KindNativeExecutor,
		Data: payload.New(&exec.NativeExecutorConfig{}),
	}

	err := manager.Delete(ctx, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_Delete_InvalidKind(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("test", "executor"),
		Kind: "invalid.kind",
		Data: payload.New(&exec.NativeExecutorConfig{}),
	}

	err := manager.Delete(ctx, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported entry kind")
}

func TestManager_RegisterFactory(t *testing.T) {
	bus := eventbus.NewBus()
	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	customFactory := &mockFactory{}
	manager.RegisterFactory(customFactory)

	assert.Equal(t, customFactory, manager.factory)
}
