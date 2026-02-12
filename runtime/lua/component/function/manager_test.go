package function

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	systempayload "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"go.uber.org/zap"
)

type mockPrepareAwaitWaiter struct {
	result event.AwaitResult
}

func (w *mockPrepareAwaitWaiter) Wait() event.AwaitResult { return w.result }
func (w *mockPrepareAwaitWaiter) Close()                  {}

type mockPrepareAwaitService struct {
	prepared bool
	result   event.AwaitResult
}

func (a *mockPrepareAwaitService) Prepare(context.Context, event.System, event.Kind, event.Path, time.Duration) (event.AwaitWaiter, error) {
	a.prepared = true
	return &mockPrepareAwaitWaiter{result: a.result}, nil
}

func (a *mockPrepareAwaitService) Await(context.Context, event.System, event.Kind, event.Path, time.Duration) event.AwaitResult {
	return event.AwaitResult{Accepted: false, Error: fmt.Errorf("unexpected Await call")}
}

func (a *mockPrepareAwaitService) Start(context.Context) error { return nil }
func (a *mockPrepareAwaitService) Stop() error                 { return nil }

func setupTestContext() context.Context {
	ctx := context.Background()
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	return payload.WithTranscoder(ctx, transcoder)
}

func TestNewManager(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()

	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	assert.NotNil(t, manager)
	assert.Equal(t, log, manager.log)
	assert.Equal(t, codeManager, manager.code)
	assert.Equal(t, bus, manager.bus)
	assert.Equal(t, disp, manager.dispatcher)
	assert.Equal(t, fsReg, manager.fsRegistry)
	assert.Equal(t, factory, manager.factory)
	assert.NotNil(t, manager.pools)
	assert.NotNil(t, manager.configs)
}

func TestManager_StartStop(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	ctx := context.Background()
	err := manager.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, manager.started)

	manager.Stop()
	assert.False(t, manager.started)
	assert.Empty(t, manager.pools)
}

func TestManager_Add_SourceFunction_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	entry := registry.Entry{
		Kind: "invalid",
	}

	err := manager.Add(context.Background(), entry)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestManager_Add_SourceFunction_InvalidConfig(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	testData := `{"source": "test", "invalid": }`
	payloadData := payload.NewPayload(testData, payload.JSON)
	entry := registry.Entry{
		Kind: api.Function,
		Data: payloadData,
	}

	ctx := setupTestContext()
	err := manager.Add(ctx, entry)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unpack function config")
}

func TestManager_Add_BytecodeFunction_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	entry := registry.Entry{
		Kind: "wrong_bytecode",
	}

	err := manager.Add(context.Background(), entry)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestManager_Add_BytecodeFunction_InvalidConfig(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	testData := `{"path": "test", "invalid": }`
	payloadData := payload.NewPayload(testData, payload.JSON)
	entry := registry.Entry{
		Kind: api.FunctionBytecode,
		Data: payloadData,
	}

	ctx := setupTestContext()
	err := manager.Add(ctx, entry)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unpack function config")
}

func TestManager_Update_SourceFunction_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	entry := registry.Entry{
		Kind: "invalid",
	}

	err := manager.Update(context.Background(), entry)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestManager_Update_BytecodeFunction_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	entry := registry.Entry{
		Kind: "wrong_bytecode",
	}

	err := manager.Update(context.Background(), entry)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestManager_Delete_SourceFunction_InvalidKind(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	entry := registry.Entry{
		Kind: "invalid",
	}

	err := manager.Delete(context.Background(), entry)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestManager_Delete_AcceptsValidKinds(t *testing.T) {
	// Verify that Delete accepts both KindFunction and KindFunctionBytecode
	// by checking Add/Update reject invalid kinds but Delete pattern is consistent
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	// Test that invalid kind is rejected
	invalidEntry := registry.Entry{
		ID:   registry.NewID("test", "func"),
		Kind: "invalid",
	}
	err := manager.Delete(context.Background(), invalidEntry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")

	// Valid kinds are tested through integration tests with properly initialized code.Manager
}

func TestManager_Invalidate_NoConfig(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	ids := []registry.ID{{Name: "test1"}, {Name: "test2"}}
	manager.Invalidate(context.Background(), ids)

	// Should not panic and factory should not be called
	assert.Equal(t, 0, factory.callCount)
}

func TestManager_Invalidate_WithSourceConfig(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	id := registry.NewID("test", "func")
	cfg := &configEntry{
		method: "main",
		pool:   api.PoolConfig{Type: api.PoolTypeInline},
		source: &api.FunctionConfig{},
	}
	manager.storeConfig(id, cfg)

	manager.Invalidate(context.Background(), []registry.ID{id})

	// Factory should be called for recompilation
	assert.Equal(t, 1, factory.callCount)
}

func TestManager_Invalidate_WithBytecodeConfig_FailsVerification(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	id := registry.NewID("test", "bytecode_func")
	cfg := &configEntry{
		method: "main",
		pool:   api.PoolConfig{Type: api.PoolTypeInline},
		bytecode: &api.BytecodeFunctionConfig{
			FS:   "test_fs",
			Path: "test.luac",
			Hash: "invalid_hash",
		},
	}
	manager.storeConfig(id, cfg)

	manager.Invalidate(context.Background(), []registry.ID{id})

	// Factory should NOT be called because bytecode verification fails
	assert.Equal(t, 0, factory.callCount)
}

func TestManager_Execute_PoolNotFound(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	task := runtime.Task{
		ID: registry.NewID("test", "nonexistent"),
	}

	result, err := manager.Execute(context.Background(), task)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "pool not found")
}

func TestManager_ConfigOperations(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	id := registry.NewID("test", "config")
	cfg := &configEntry{
		method: "main",
		pool:   api.PoolConfig{Workers: 4},
	}

	// Store config
	manager.storeConfig(id, cfg)

	// Get config
	retrieved := manager.getConfig(id)
	require.NotNil(t, retrieved)
	assert.Equal(t, "main", retrieved.method)
	assert.Equal(t, 4, retrieved.pool.Workers)

	// Delete config
	manager.deleteConfig(id)

	// Verify deleted
	deleted := manager.getConfig(id)
	assert.Nil(t, deleted)
}

func TestManager_ConfigOperations_NonExistent(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	id := registry.NewID("test", "nonexistent")
	cfg := manager.getConfig(id)
	assert.Nil(t, cfg)
}

func TestManager_registerCaller_PreparesBeforeSend(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	awaitSvc := &mockPrepareAwaitService{
		result: event.AwaitResult{Accepted: true},
	}
	sendBeforePrepare := false
	bus.onSend = func() {
		if !awaitSvc.prepared {
			sendBeforePrepare = true
		}
	}

	ctx := event.WithAwaitService(ctxapi.NewRootContext(), awaitSvc)
	err := manager.registerCaller(ctx, registry.NewID("app.test", "function"), nil)
	require.NoError(t, err)
	assert.False(t, sendBeforePrepare, "function register was sent before await prepare")
}

func TestManager_PoolTypes(t *testing.T) {
	tests := []struct {
		name     string
		poolType string
	}{
		{"inline", api.PoolTypeInline},
		{"lazy", api.PoolTypeLazy},
		{"static", api.PoolTypeStatic},
		{"adaptive", api.PoolTypeAdaptive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := zap.NewNop()
			codeManager := &code.Manager{}
			bus := newMockEventBus()
			disp := &mockDispatcher{}
			fsReg := newMockFSRegistry()
			factory := newMockCompiledFactory()
			manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

			id := registry.NewID("test", tt.name)
			cfg := &configEntry{
				method: "main",
				pool: api.PoolConfig{
					Type:    tt.poolType,
					Workers: 4,
					MaxSize: 8,
					Size:    4,
					Buffer:  256,
				},
			}

			err := manager.createPool(id, cfg)
			assert.NoError(t, err)

			// Verify pool was created
			manager.mu.RLock()
			entry, exists := manager.pools[id]
			manager.mu.RUnlock()

			assert.True(t, exists)
			assert.NotNil(t, entry)
			assert.Equal(t, "main", entry.method)

			// Cleanup
			manager.removePool(id)
		})
	}
}

func TestManager_AutoSelectPool(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		pool     api.PoolConfig
	}{
		{"lazy when workers=0 size=0", "lazy", api.PoolConfig{Workers: 0, Size: 0}},
		{"lazy when workers=0 maxsize>0", "lazy", api.PoolConfig{Workers: 0, MaxSize: 10}},
		{"static when workers>0", "static", api.PoolConfig{Workers: 4}},
		{"inline when size>0 workers=0", "inline", api.PoolConfig{Size: 4, Workers: 0, MaxSize: 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := zap.NewNop()
			codeManager := &code.Manager{}
			bus := newMockEventBus()
			disp := &mockDispatcher{}
			fsReg := newMockFSRegistry()
			factory := newMockCompiledFactory()
			manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

			id := registry.NewID("test", "auto")
			cfg := &configEntry{
				method: "main",
				pool:   tt.pool,
			}

			err := manager.createPool(id, cfg)
			assert.NoError(t, err, "pool type: %s", tt.expected)

			manager.removePool(id)
		})
	}
}

func TestManager_ReplacePool(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	id := registry.NewID("test", "replace")
	cfg := &configEntry{
		method: "main",
		pool:   api.PoolConfig{Type: api.PoolTypeInline},
	}

	// Create initial pool
	err := manager.createPool(id, cfg)
	require.NoError(t, err)

	// Replace pool
	newCfg := &configEntry{
		method: "updated",
		pool:   api.PoolConfig{Type: api.PoolTypeLazy, MaxSize: 8},
	}
	err = manager.replacePool(id, newCfg)
	require.NoError(t, err)

	// Verify pool was replaced
	manager.mu.RLock()
	entry, exists := manager.pools[id]
	manager.mu.RUnlock()

	assert.True(t, exists)
	assert.Equal(t, "updated", entry.method)

	// Factory should have been called twice
	assert.Equal(t, 2, factory.callCount)

	manager.removePool(id)
}

func TestManager_RemovePool_NonExistent(_ *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	// Should not panic
	manager.removePool(registry.NewID("test", "nonexistent"))
}

func TestManager_CreatePool_FactoryError(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	factory.shouldFail = true
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	id := registry.NewID("test", "fail")
	cfg := &configEntry{
		method: "main",
		pool:   api.PoolConfig{Type: api.PoolTypeInline},
	}

	err := manager.createPool(id, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compile")
}

func TestManager_CreatePool_UnknownPoolType(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	id := registry.NewID("test", "unknown")
	cfg := &configEntry{
		method: "main",
		pool:   api.PoolConfig{Type: "unknown_type"},
	}

	err := manager.createPool(id, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create pool")
}

func TestManager_Stop_WithActivePools(t *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	// Start manager
	err := manager.Start(context.Background())
	require.NoError(t, err)

	// Create multiple pools
	for i := 0; i < 3; i++ {
		id := registry.NewID("test", "pool"+strconv.Itoa(i))
		cfg := &configEntry{
			method: "main",
			pool:   api.PoolConfig{Type: api.PoolTypeInline},
		}
		err := manager.createPool(id, cfg)
		require.NoError(t, err)
	}

	assert.Len(t, manager.pools, 3)

	// Stop should clean up all pools
	manager.Stop()
	assert.Empty(t, manager.pools)
	assert.False(t, manager.started)
}

func TestManager_Concurrency(_ *testing.T) {
	log := zap.NewNop()
	codeManager := &code.Manager{}
	bus := newMockEventBus()
	disp := &mockDispatcher{}
	fsReg := newMockFSRegistry()
	factory := newMockCompiledFactory()
	manager := NewManager(log, codeManager, bus, disp, fsReg, factory)

	done := make(chan struct{})

	// Concurrent config operations
	go func() {
		for i := 0; i < 100; i++ {
			id := registry.NewID("test", "concurrent")
			cfg := &configEntry{method: "test"}
			manager.storeConfig(id, cfg)
			manager.getConfig(id)
			manager.deleteConfig(id)
		}
		done <- struct{}{}
	}()

	// Concurrent pool operations
	go func() {
		for i := 0; i < 50; i++ {
			id := registry.NewID("test", "pool_concurrent")
			cfg := &configEntry{
				method: "main",
				pool:   api.PoolConfig{Type: api.PoolTypeInline},
			}
			_ = manager.createPool(id, cfg)
			manager.removePool(id)
		}
		done <- struct{}{}
	}()

	<-done
	<-done
}
