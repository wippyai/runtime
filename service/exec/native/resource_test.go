package native

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/service/exec"
)

type mockExecutor struct {
	process exec.Process
}

func (m *mockExecutor) NewProcess(_ string, _ exec.ProcessOptions) (exec.Process, error) {
	return m.process, nil
}

func TestExecutorProvider_Acquire(t *testing.T) {
	executor := &mockExecutor{}
	provider := newExecutorProvider(executor)

	ctx := context.Background()
	id := registry.NewID("test", "executor")

	res, err := provider.Acquire(ctx, id, resource.ModeNormal)
	require.NoError(t, err)
	assert.NotNil(t, res)

	val, err := res.Get()
	require.NoError(t, err)
	assert.Equal(t, executor, val)

	res.Release()

	val, err = res.Get()
	assert.ErrorIs(t, err, resource.ErrReleased)
	assert.Nil(t, val)
}

func TestExecutorProvider_AcquireExclusive(t *testing.T) {
	executor := &mockExecutor{}
	provider := newExecutorProvider(executor)

	ctx := context.Background()
	id := registry.NewID("test", "executor")

	res, err := provider.Acquire(ctx, id, resource.ModeExclusive)
	assert.ErrorIs(t, err, resource.ErrLocked)
	assert.Nil(t, res)
}

func TestExecutorProvider_Close(t *testing.T) {
	executor := &mockExecutor{}
	provider := newExecutorProvider(executor)

	ctx := context.Background()
	id := registry.NewID("test", "executor")

	err := provider.Close()
	require.NoError(t, err)

	res, err := provider.Acquire(ctx, id, resource.ModeNormal)
	assert.ErrorIs(t, err, resource.ErrClosed)
	assert.Nil(t, res)

	err = provider.Close()
	require.NoError(t, err)
}

func TestExecutorResource_MultipleRelease(t *testing.T) {
	executor := &mockExecutor{}
	provider := newExecutorProvider(executor)

	ctx := context.Background()
	id := registry.NewID("test", "executor")

	res, err := provider.Acquire(ctx, id, resource.ModeNormal)
	require.NoError(t, err)

	res.Release()
	res.Release()
	res.Release()
}
