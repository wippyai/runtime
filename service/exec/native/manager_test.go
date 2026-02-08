package native

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

type mockTranscoder struct{}

func (m *mockTranscoder) Unmarshal(p payload.Payload, v any) error {
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

func requireAPIError(t *testing.T, err error, kind apierror.Kind, msg string) apierror.Error {
	t.Helper()
	require.Error(t, err)
	var apiErr apierror.Error
	ok := errors.As(err, &apiErr)
	require.Truef(t, ok, "expected apierror.Error, got %T", err)
	assert.Equal(t, kind, apiErr.Kind())
	assert.Contains(t, err.Error(), msg)
	return apiErr
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
		Kind: exec.NativeExecutor,
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
		Kind: exec.NativeExecutor,
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	err = manager.Add(ctx, entry)
	apiErr := requireAPIError(t, err, apierror.AlreadyExists, "executor already exists")
	assert.Equal(t, entry.ID.String(), apiErr.Details().GetString("executor_id", ""))
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
	apiErr := requireAPIError(t, err, apierror.Invalid, "unsupported entry kind")
	assert.Equal(t, "invalid.kind", apiErr.Details().GetString("kind", ""))
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
		Kind: exec.NativeExecutor,
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	newConfig := &exec.NativeExecutorConfig{
		DefaultWorkDir: "/var/tmp",
	}

	updatedEntry := registry.Entry{
		ID:   registry.NewID("test", "executor"),
		Kind: exec.NativeExecutor,
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
		Kind: exec.NativeExecutor,
		Data: payload.New(config),
	}

	err := manager.Update(ctx, entry)
	apiErr := requireAPIError(t, err, apierror.NotFound, "executor not found")
	assert.Equal(t, entry.ID.String(), apiErr.Details().GetString("executor_id", ""))
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
	apiErr := requireAPIError(t, err, apierror.Invalid, "unsupported entry kind")
	assert.Equal(t, "invalid.kind", apiErr.Details().GetString("kind", ""))
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
		Kind: exec.NativeExecutor,
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
		Kind: exec.NativeExecutor,
		Data: payload.New(&exec.NativeExecutorConfig{}),
	}

	err := manager.Delete(ctx, entry)
	apiErr := requireAPIError(t, err, apierror.NotFound, "executor not found")
	assert.Equal(t, entry.ID.String(), apiErr.Details().GetString("executor_id", ""))
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
	apiErr := requireAPIError(t, err, apierror.Invalid, "unsupported entry kind")
	assert.Equal(t, "invalid.kind", apiErr.Details().GetString("kind", ""))
}

func TestManager_RegisterFactory(t *testing.T) {
	bus := eventbus.NewBus()
	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())

	customFactory := &mockFactory{}
	manager.RegisterFactory(customFactory)

	assert.Equal(t, customFactory, manager.factory)
}
