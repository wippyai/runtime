package docker

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/system/eventbus"
	systemresource "github.com/wippyai/runtime/system/resource"
	"go.uber.org/zap"
)

type mockTranscoder struct{}

func (m *mockTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
	if cfg, ok := v.(*exec.DockerExecutorConfig); ok {
		if src, ok := p.Data().(*exec.DockerExecutorConfig); ok {
			*cfg = *src
			return nil
		}
	}
	return nil
}

func (m *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

type mockExecutor struct{}

func (e *mockExecutor) NewProcess(_ string, _ exec.ProcessOptions) (exec.Process, error) {
	return &mockProcess{}, nil
}

type mockProcess struct{}

func (p *mockProcess) Start() error              { return nil }
func (p *mockProcess) Signal(_ int) error        { return nil }
func (p *mockProcess) WriteStdin(_ []byte) error { return nil }
func (p *mockProcess) Stdout() io.ReadCloser     { return nil }
func (p *mockProcess) Stderr() io.ReadCloser     { return nil }
func (p *mockProcess) Wait() error               { return nil }

type mockFactory struct {
	createErr error
}

func (f *mockFactory) CreateExecutor(_ registry.ID, _ *exec.DockerExecutorConfig) (exec.ProcessExecutor, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &mockExecutor{}, nil
}

func TestManager_Add(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())
	manager.factory = &mockFactory{}

	config := &exec.DockerExecutorConfig{
		Image: "alpine:latest",
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "executor"),
		Kind: exec.KindDockerExecutor,
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
	manager.factory = &mockFactory{}

	config := &exec.DockerExecutorConfig{
		Image: "alpine:latest",
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "executor"),
		Kind: exec.KindDockerExecutor,
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
		Data: payload.New(&exec.DockerExecutorConfig{}),
	}

	err := manager.Add(ctx, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported entry kind")
}

func TestManager_Add_FactoryError(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())
	manager.factory = &mockFactory{createErr: errors.New("factory error")}

	config := &exec.DockerExecutorConfig{
		Image: "alpine:latest",
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "executor"),
		Kind: exec.KindDockerExecutor,
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create executor")
}

func TestManager_Update(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	bus := eventbus.NewBus()

	manager := NewManager(bus, &mockTranscoder{}, zap.NewNop())
	manager.factory = &mockFactory{}

	config := &exec.DockerExecutorConfig{
		Image: "alpine:latest",
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "executor"),
		Kind: exec.KindDockerExecutor,
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	newConfig := &exec.DockerExecutorConfig{
		Image: "ubuntu:latest",
	}

	updatedEntry := registry.Entry{
		ID:   registry.NewID("test", "executor"),
		Kind: exec.KindDockerExecutor,
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

	config := &exec.DockerExecutorConfig{
		Image: "alpine:latest",
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "executor"),
		Kind: exec.KindDockerExecutor,
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
		Data: payload.New(&exec.DockerExecutorConfig{}),
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
	manager.factory = &mockFactory{}

	config := &exec.DockerExecutorConfig{
		Image: "alpine:latest",
	}

	entry := registry.Entry{
		ID:   registry.NewID("test", "executor"),
		Kind: exec.KindDockerExecutor,
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
		Kind: exec.KindDockerExecutor,
		Data: payload.New(&exec.DockerExecutorConfig{}),
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
		Data: payload.New(&exec.DockerExecutorConfig{}),
	}

	err := manager.Delete(ctx, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported entry kind")
}

func TestExecutorProvider_Acquire(t *testing.T) {
	provider := newExecutorProvider(&mockExecutor{})

	res, err := provider.Acquire(context.Background(), registry.ID{}, resource.ModeNormal)
	require.NoError(t, err)
	assert.NotNil(t, res)

	value, err := res.Get()
	require.NoError(t, err)
	assert.NotNil(t, value)

	res.Release()
}

func TestExecutorProvider_Acquire_Exclusive(t *testing.T) {
	provider := newExecutorProvider(&mockExecutor{})

	_, err := provider.Acquire(context.Background(), registry.ID{}, resource.ModeExclusive)
	assert.ErrorIs(t, err, systemresource.ErrLocked)
}

func TestExecutorProvider_Acquire_Closed(t *testing.T) {
	provider := newExecutorProvider(&mockExecutor{})
	provider.Close()

	_, err := provider.Acquire(context.Background(), registry.ID{}, resource.ModeNormal)
	assert.ErrorIs(t, err, systemresource.ErrClosed)
}

func TestExecutorResource_Released(t *testing.T) {
	res := &executorResource{executor: &mockExecutor{}}
	res.Release()

	_, err := res.Get()
	assert.ErrorIs(t, err, resource.ErrReleased)
}
