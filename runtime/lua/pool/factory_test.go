package pool

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/pool/queued"
	pool_sync "github.com/ponyruntime/pony/runtime/lua/pool/sync"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua_vm "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Mock VM implementation
type mockVM struct {
	mu     sync.Mutex
	closed bool
}

func (m *mockVM) Execute(_ context.Context, _ string, _ ...lua_vm.LValue) (lua_vm.LValue, error) {
	return lua_vm.LNil, nil
}

func (m *mockVM) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}

// Mock Factory implementation
type mockFactory struct {
	mu            sync.Mutex
	makeVMCalled  bool
	compileCalled bool
}

func (m *mockFactory) MakeVM() (lua.VM, error) {
	m.mu.Lock()
	m.makeVMCalled = true
	m.mu.Unlock()
	return &mockVM{}, nil
}

func (m *mockFactory) Compile() error {
	m.mu.Lock()
	m.compileCalled = true
	m.mu.Unlock()
	return nil
}

func (m *mockFactory) WasMakeVMCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.makeVMCalled
}

func TestFactory_Build(t *testing.T) {
	logger := zap.NewNop()

	t.Run("builds queued pool when workers specified", func(t *testing.T) {
		factory := NewFactory(logger)
		mockLuaFactory := &mockFactory{}

		cfg := &lua.FunctionConfig{
			Pool: lua.PoolConfig{
				Size:    5,
				Workers: 3,
			},
		}

		callable, err := factory.Build(mockLuaFactory, cfg)
		require.NoError(t, err)

		// Clean up the pool right away to avoid goroutine leaks
		defer callable.(*queued.Pool).Close()

		// Verify we got a queued pool
		queuedPool, ok := callable.(*queued.Pool)
		assert.True(t, ok, "expected queued pool")
		assert.NotNil(t, queuedPool)

		// Give workers time to initialize
		_, err = queuedPool.Execute(context.Background(), "test")
		require.NoError(t, err)

		// Verify the factory was called
		assert.Eventually(t, mockLuaFactory.WasMakeVMCalled, 100*time.Millisecond, 10*time.Millisecond,
			"factory should create VMs")
	})

	t.Run("builds sync pool when no workers specified", func(t *testing.T) {
		factory := NewFactory(logger)
		mockLuaFactory := &mockFactory{}

		cfg := &lua.FunctionConfig{
			Pool: lua.PoolConfig{
				Size:    5,
				Workers: 0,
			},
		}

		callable, err := factory.Build(mockLuaFactory, cfg)
		require.NoError(t, err)
		defer callable.(*pool_sync.Pool).Close()

		// Verify we got a sync pool
		syncPool, ok := callable.(*pool_sync.Pool)
		assert.True(t, ok, "expected sync pool")
		assert.NotNil(t, syncPool)
	})

	t.Run("handles zero pool size", func(t *testing.T) {
		factory := NewFactory(logger)
		mockLuaFactory := &mockFactory{}

		cfg := &lua.FunctionConfig{
			Pool: lua.PoolConfig{
				Size:    0,
				Workers: 0,
			},
		}

		callable, err := factory.Build(mockLuaFactory, cfg)
		require.NoError(t, err)
		defer callable.(*pool_sync.Pool).Close()
		assert.NotNil(t, callable, "should create pool with default size")
	})

	t.Run("handles negative workers", func(t *testing.T) {
		factory := NewFactory(logger)
		mockLuaFactory := &mockFactory{}

		cfg := &lua.FunctionConfig{
			Pool: lua.PoolConfig{
				Size:    5,
				Workers: -1,
			},
		}

		callable, err := factory.Build(mockLuaFactory, cfg)
		require.NoError(t, err)
		defer callable.(*pool_sync.Pool).Close()

		// Negative workers should result in sync pool
		_, ok := callable.(*pool_sync.Pool)
		assert.True(t, ok, "expected sync pool with negative workers")
	})

	t.Run("handles negative size", func(t *testing.T) {
		factory := NewFactory(logger)
		mockLuaFactory := &mockFactory{}

		cfg := &lua.FunctionConfig{
			Pool: lua.PoolConfig{
				Size:    -1,
				Workers: 3,
			},
		}

		callable, err := factory.Build(mockLuaFactory, cfg)
		require.NoError(t, err)
		defer callable.(*queued.Pool).Close()
		assert.NotNil(t, callable, "should create pool with default size")
	})
}
