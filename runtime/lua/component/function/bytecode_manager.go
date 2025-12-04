// Package function provides Lua function management.
package function

import (
	"context"
	"sync"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/component"
	"github.com/wippyai/runtime/runtime/lua/engine"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	funcpool "github.com/wippyai/runtime/system/scheduler/pool"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// bytecodePoolEntry wraps a pool with its config for bytecode functions.
type bytecodePoolEntry struct {
	pool   funcpool.Pool
	config api.BytecodeFunctionConfig
	method string
}

// BytecodeManager handles precompiled Lua bytecode function loading and execution.
type BytecodeManager struct {
	ctx        context.Context
	log        *zap.Logger
	code       *code.Manager
	bus        event.Bus
	dispatcher dispatcher.Dispatcher
	fsRegistry fsapi.Registry
	topo       topology.Topology
	pidReg     topology.PIDRegistry
	node       relayapi.Node

	mu      sync.RWMutex
	pools   map[registry.ID]*bytecodePoolEntry
	configs sync.Map
	started bool
}

// NewBytecodeManager creates a new bytecode function manager.
func NewBytecodeManager(
	log *zap.Logger,
	code *code.Manager,
	bus event.Bus,
	disp dispatcher.Dispatcher,
	fsReg fsapi.Registry,
) *BytecodeManager {
	return &BytecodeManager{
		log:        log,
		code:       code,
		bus:        bus,
		dispatcher: disp,
		fsRegistry: fsReg,
		pools:      make(map[registry.ID]*bytecodePoolEntry),
	}
}

// Start marks the manager as ready to accept pools.
func (m *BytecodeManager) Start(ctx context.Context) error {
	m.ctx = ctx
	m.topo = topology.GetTopology(ctx)
	m.pidReg = topology.GetRegistry(ctx)
	m.node = relayapi.GetNode(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
	m.log.Info("bytecode function manager started")
	return nil
}

// Stop stops all pools gracefully.
func (m *BytecodeManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, entry := range m.pools {
		entry.pool.Stop()
		if m.node != nil {
			m.node.UnregisterHost(id.String())
		}
		m.log.Debug("bytecode pool stopped", zap.String("id", id.String()))
	}
	m.pools = make(map[registry.ID]*bytecodePoolEntry)
	m.started = false
	m.log.Info("bytecode function manager stopped")
}

// Add loads and registers a new bytecode function.
func (m *BytecodeManager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindFunctionBytecode {
		return NewInvalidEntryKindError(string(entry.Kind), string(api.KindFunctionBytecode))
	}

	cfg, err := component.UnpackConfig[api.BytecodeFunctionConfig](ctx, entry)
	if err != nil {
		return NewUnpackConfigError(err)
	}

	proto, err := component.LoadAndVerifyBytecode(m.fsRegistry, cfg.FS, cfg.Path, cfg.Hash)
	if err != nil {
		return NewLoadBytecodeError(err)
	}

	// Add to code manager with the proto (no source needed)
	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindFunctionBytecode,
		Method: cfg.Method,
	}
	imports := component.BuildImports(cfg.Imports, cfg.Modules)
	if err := m.code.AddNode(ctx, node, imports); err != nil {
		return NewAddFunctionError(err)
	}

	// Create pool with the loaded proto
	if err := m.createPool(ctx, entry.ID, cfg, proto); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		return NewCreatePoolError(err)
	}

	// Store config
	m.configs.Store(entry.ID, cfg)

	// Register function caller
	opts, _ := cfg.Meta.GetBag("options")
	m.registerCaller(ctx, entry.ID, opts)

	m.log.Info("bytecode function added",
		zap.String("id", entry.ID.String()),
		zap.String("fs", cfg.FS),
		zap.String("path", cfg.Path),
	)

	return nil
}

// Update updates an existing bytecode function.
func (m *BytecodeManager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindFunctionBytecode {
		return NewInvalidEntryKindError(string(entry.Kind), string(api.KindFunctionBytecode))
	}

	cfg, err := component.UnpackConfig[api.BytecodeFunctionConfig](ctx, entry)
	if err != nil {
		return NewUnpackConfigError(err)
	}

	proto, err := component.LoadAndVerifyBytecode(m.fsRegistry, cfg.FS, cfg.Path, cfg.Hash)
	if err != nil {
		return NewLoadBytecodeError(err)
	}

	// Update code manager
	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindFunctionBytecode,
		Method: cfg.Method,
	}
	imports := component.BuildImports(cfg.Imports, cfg.Modules)
	if err := m.code.UpdateNode(ctx, node, imports); err != nil {
		return NewUpdateFunctionNodeError(err)
	}

	if err := m.replacePool(ctx, entry.ID, cfg, proto); err != nil {
		return NewReplacePoolError(err)
	}

	m.configs.Store(entry.ID, cfg)

	opts, _ := cfg.Meta.GetBag("options")
	m.registerCaller(ctx, entry.ID, opts)

	m.log.Info("bytecode function updated", zap.String("id", entry.ID.String()))
	return nil
}

// Delete removes a bytecode function.
func (m *BytecodeManager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindFunctionBytecode {
		return NewInvalidEntryKindError(string(entry.Kind), string(api.KindFunctionBytecode))
	}

	if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
		return NewDeleteFunctionNodeError(err)
	}

	m.removePool(entry.ID)
	m.configs.Delete(entry.ID)
	m.unregisterCaller(ctx, entry.ID)

	m.log.Info("bytecode function deleted", zap.String("id", entry.ID.String()))
	return nil
}

// Invalidate handles code invalidation for hot reload.
func (m *BytecodeManager) Invalidate(ctx context.Context, ids []registry.ID) {
	for _, id := range ids {
		cfgAny, exists := m.configs.Load(id)
		if !exists {
			continue
		}
		cfg := cfgAny.(*api.BytecodeFunctionConfig)

		m.log.Debug("invalidating bytecode function", zap.String("id", id.String()))

		proto, err := component.LoadAndVerifyBytecode(m.fsRegistry, cfg.FS, cfg.Path, cfg.Hash)
		if err != nil {
			m.log.Error("failed to reload bytecode", zap.Error(err))
			continue
		}

		if err := m.replacePool(ctx, id, cfg, proto); err != nil {
			m.log.Error("failed to invalidate bytecode function", zap.Error(err))
		}
	}
}

// Execute runs a bytecode function with given task.
func (m *BytecodeManager) Execute(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	m.mu.RLock()
	entry, exists := m.pools[task.ID]
	m.mu.RUnlock()

	if !exists {
		return nil, NewPoolNotFoundError(task.ID.String())
	}

	if len(task.Context) > 0 {
		fc := ctxapi.FrameFromContext(ctx)
		if fc != nil {
			for _, pair := range task.Context {
				_ = fc.Set(pair.Key, pair.Value)
			}
		}
	}

	result, err := entry.pool.Call(ctx, entry.method, task.Payloads)
	return result, err
}

// createPool creates a new pool for a bytecode function.
func (m *BytecodeManager) createPool(ctx context.Context, id registry.ID, cfg *api.BytecodeFunctionConfig, proto *lua.FunctionProto) error {
	// Get compiled dependencies from code manager
	compiled, err := m.code.Compile(id, functionBuildOptions())
	if err != nil {
		return NewCompileError(err)
	}

	// Create process factory with the loaded proto
	factory := m.createFactory(proto, compiled)

	workers := cfg.Pool.Workers
	if workers == 0 {
		workers = cfg.Pool.Size
	}
	if workers == 0 {
		workers = 8
	}
	queueSize := cfg.Pool.Buffer
	if queueSize == 0 {
		queueSize = workers * 64
	}

	var pool funcpool.Pool
	poolType := cfg.Pool.Type
	if poolType == "" {
		poolType = api.PoolTypeInline
	}

	maxWorkers := cfg.Pool.MaxSize
	if maxWorkers == 0 {
		maxWorkers = 16
	}

	execHooks := m.createExecutionHooks()

	switch poolType {
	case api.PoolTypeInline:
		pool, err = funcpool.NewInline(factory, m.dispatcher, execHooks)

	case api.PoolTypeLazy:
		pool, err = funcpool.NewLazy(factory, m.dispatcher, funcpool.LazyConfig{
			MaxWorkers:  maxWorkers,
			IdleTimeout: 30 * time.Second,
		}, execHooks)

	case api.PoolTypeStatic:
		pool, err = funcpool.NewStatic(factory, m.dispatcher, funcpool.Config{
			Workers:   workers,
			QueueSize: queueSize,
		}, execHooks)

	default:
		return NewUnknownPoolTypeError(string(poolType))
	}

	if err != nil {
		return NewCreatePoolError(err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.pools[id] = &bytecodePoolEntry{
		pool:   pool,
		config: *cfg,
		method: cfg.Method,
	}

	if m.node != nil {
		if err := m.node.RegisterHost(id.String(), pool); err != nil {
			m.log.Warn("failed to register bytecode pool as host", zap.String("id", id.String()), zap.Error(err))
		}
	}

	if m.started {
		pool.Start()
	}

	return nil
}

// replacePool stops old pool and creates new one.
func (m *BytecodeManager) replacePool(ctx context.Context, id registry.ID, cfg *api.BytecodeFunctionConfig, proto *lua.FunctionProto) error {
	m.removePool(id)
	return m.createPool(ctx, id, cfg, proto)
}

// removePool stops and removes a pool.
func (m *BytecodeManager) removePool(id registry.ID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, exists := m.pools[id]; exists {
		entry.pool.Stop()
		delete(m.pools, id)

		if m.node != nil {
			m.node.UnregisterHost(id.String())
		}
	}
}

// createFactory creates a ProcessFactory from the bytecode proto and compiled dependencies.
func (m *BytecodeManager) createFactory(proto *lua.FunctionProto, compiled *code.CompiledMain) funcpool.Factory {
	return func() (process.Process, error) {
		return createBytecodeProcess(proto, compiled)
	}
}

// createBytecodeProcess creates a new process with the bytecode proto and dependencies.
func createBytecodeProcess(proto *lua.FunctionProto, compiled *code.CompiledMain) (process.Process, error) {
	binders := engine.CoreBinders()
	binders = append(binders, processmod.BindGlobal)

	// Add module binders for dependencies
	for _, dep := range compiled.Dependencies {
		if dep.Node != nil && dep.Node.Module != nil {
			mod := dep.Node.Module
			name := dep.Name
			binders = append(binders, func(L *lua.LState) {
				L.PreloadModule(name, func(L *lua.LState) int {
					return mod.Loader(L)
				})
			})
		}
		if dep.Proto != nil {
			depProto := dep.Proto
			name := dep.Name
			binders = append(binders, func(L *lua.LState) {
				L.PreloadModule(name, func(L *lua.LState) int {
					fn := L.LoadProto(depProto)
					L.Push(fn)
					L.Call(0, 1)
					return 1
				})
			})
		}
	}

	// Add preloaded modules
	for _, pre := range compiled.Preloaded {
		if pre.Node != nil && pre.Node.Module != nil {
			mod := pre.Node.Module
			name := pre.Name
			binders = append(binders, func(L *lua.LState) {
				L.PreloadModule(name, func(L *lua.LState) int {
					return mod.Loader(L)
				})
			})
		}
		if pre.Proto != nil {
			preProto := pre.Proto
			name := pre.Name
			binders = append(binders, func(L *lua.LState) {
				L.PreloadModule(name, func(L *lua.LState) int {
					fn := L.LoadProto(preProto)
					L.Push(fn)
					L.Call(0, 1)
					return 1
				})
			})
		}
	}

	cfg := engine.FactoryConfig{
		Proto:         proto,
		ModuleBinders: binders,
	}

	factory := engine.NewFactory(cfg)
	return factory()
}

// registerCaller registers function in the function system.
func (m *BytecodeManager) registerCaller(ctx context.Context, id registry.ID, options runtime.Options) {
	m.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   id.String(),
		Data: &function.FuncEntry{
			Handler: m.Execute,
			Options: options,
		},
	})
}

// unregisterCaller removes function from the function system.
func (m *BytecodeManager) unregisterCaller(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Delete,
		Path:   id.String(),
	})
}

// createExecutionHooks creates execution hooks for topology integration.
func (m *BytecodeManager) createExecutionHooks() funcpool.ExecutionHooks {
	if m.topo == nil || m.pidReg == nil {
		return funcpool.ExecutionHooks{}
	}

	onStart := func(ctx context.Context, _ process.Process) {
		pid, ok := runtime.GetFramePID(ctx)
		if !ok || pid.String() == "" {
			return
		}

		if err := m.topo.Register(pid); err != nil {
			m.log.Warn("failed to register bytecode function PID in topology",
				zap.String("pid", pid.String()),
				zap.Error(err))
		}
	}

	onComplete := func(ctx context.Context, result *runtime.Result) {
		pid, ok := runtime.GetFramePID(ctx)
		if !ok || pid.String() == "" {
			return
		}

		if result.Error != nil {
			if result.Error == supervisor.ErrExit {
				result.Error = nil
			}
		}

		m.topo.Notify(pid, result)
		m.pidReg.Remove(pid)
		m.topo.Remove(pid)
	}

	return funcpool.ExecutionHooks{
		OnStart:    onStart,
		OnComplete: onComplete,
	}
}

// Compile-time check
var _ registry.EntryListener = (*BytecodeManager)(nil)
