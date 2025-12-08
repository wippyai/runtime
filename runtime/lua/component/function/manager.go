// Package function provides Lua function management.
// Uses pluggable pool schedulers for different workload patterns.
package function

import (
	"context"
	"sync"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/component"
	"github.com/wippyai/runtime/runtime/lua/engine"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	funcpool "github.com/wippyai/runtime/system/scheduler/pool"
	lua "github.com/yuin/gopher-lua"

	api "github.com/wippyai/runtime/api/runtime/lua"
	"go.uber.org/zap"
)

// poolEntry wraps a pool with its config.
type poolEntry struct {
	pool   funcpool.Pool
	config api.PoolConfig
	method string
}

// Manager handles Lua function compilation, pooling and execution.
type Manager struct {
	ctx        context.Context
	log        *zap.Logger
	code       *code.Manager
	bus        event.Bus
	dispatcher dispatcher.Dispatcher
	topo       topology.Topology
	pidReg     topology.PIDRegistry
	node       relay.Node

	pools   sync.Map // map[registry.ID]*poolEntry - lock-free for hot path
	configs sync.Map
	mu      sync.Mutex // only for start/stop coordination
	started bool
}

// NewManager creates a new function manager.
func NewManager(log *zap.Logger, code *code.Manager, bus event.Bus, disp dispatcher.Dispatcher) *Manager {
	return &Manager{
		log:        log,
		code:       code,
		bus:        bus,
		dispatcher: disp,
	}
}

// Start marks the manager as ready to accept pools.
func (m *Manager) Start(ctx context.Context) error {
	m.ctx = ctx
	m.topo = topology.GetTopology(ctx)
	m.pidReg = topology.GetRegistry(ctx)
	m.node = relay.GetNode(ctx)

	m.mu.Lock()
	m.started = true
	m.mu.Unlock()

	m.log.Info("function manager started")
	return nil
}

// Stop stops all pools gracefully.
func (m *Manager) Stop() {
	m.mu.Lock()
	m.started = false
	m.mu.Unlock()

	m.pools.Range(func(key, value any) bool {
		id := key.(registry.ID)
		entry := value.(*poolEntry)
		entry.pool.Stop()
		if m.node != nil {
			m.node.UnregisterHost(id.String())
		}
		m.pools.Delete(id)
		m.log.Debug("pool stopped", zap.String("id", id.String()))
		return true
	})

	m.log.Info("function manager stopped")
}

// Add creates and registers a new function.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindFunction {
		return api.NewInvalidEntryKindError(string(entry.Kind), string(api.KindFunction))
	}

	cfg, err := component.UnpackConfig[api.FunctionConfig](ctx, entry)
	if err != nil {
		return api.NewUnpackConfigError("function", err)
	}

	// Add to code manager
	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindFunction,
		Source: cfg.Source,
		Method: cfg.Method,
	}
	imports := component.BuildImports(cfg.Imports, cfg.Modules)
	if err := m.code.AddNode(ctx, node, imports); err != nil {
		return api.NewAddNodeError("function", err)
	}

	if err := m.createPool(entry.ID, cfg); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		return api.NewCreatePoolError(err)
	}

	// Store config for invalidation
	m.configs.Store(entry.ID, cfg)

	// Register function caller
	opts, _ := cfg.Meta.GetBag("options")
	m.registerCaller(ctx, entry.ID, opts)

	m.log.Info("function added",
		zap.String("id", entry.ID.String()),
		zap.Int("workers", cfg.Pool.Workers),
	)

	return nil
}

// Update updates an existing function.
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindFunction {
		return api.NewInvalidEntryKindError(string(entry.Kind), string(api.KindFunction))
	}

	cfg, err := component.UnpackConfig[api.FunctionConfig](ctx, entry)
	if err != nil {
		return api.NewUnpackConfigError("function", err)
	}

	// Update code manager
	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindFunction,
		Source: cfg.Source,
		Method: cfg.Method,
	}
	imports := component.BuildImports(cfg.Imports, cfg.Modules)
	if err := m.code.UpdateNode(ctx, node, imports); err != nil {
		return api.NewUpdateNodeError("function", err)
	}

	if err := m.replacePool(entry.ID, cfg); err != nil {
		return api.NewReplacePoolError(err)
	}

	// Update config
	m.configs.Store(entry.ID, cfg)

	// Re-register function caller
	opts, _ := cfg.Meta.GetBag("options")
	m.registerCaller(ctx, entry.ID, opts)

	m.log.Info("function updated", zap.String("id", entry.ID.String()))
	return nil
}

// Delete removes a function.
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindFunction {
		return api.NewInvalidEntryKindError(entry.Kind, api.KindFunction)
	}

	if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
		return api.NewDeleteNodeError("function", err)
	}

	// Stop and remove pool
	m.removePool(entry.ID)

	// Remove config
	m.configs.Delete(entry.ID)

	// Unregister function caller
	m.unregisterCaller(ctx, entry.ID)

	m.log.Info("function deleted", zap.String("id", entry.ID.String()))
	return nil
}

// Invalidate handles code invalidation for hot reload.
func (m *Manager) Invalidate(_ context.Context, ids []registry.ID) {
	for _, id := range ids {
		cfgAny, exists := m.configs.Load(id)
		if !exists {
			continue
		}
		cfg := cfgAny.(*api.FunctionConfig)

		m.log.Debug("invalidating function", zap.String("id", id.String()))

		if err := m.replacePool(id, cfg); err != nil {
			m.log.Error("failed to invalidate function", zap.Error(err))
		}
	}
}

// Execute runs a function with given task.
func (m *Manager) Execute(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	v, exists := m.pools.Load(task.ID)
	if !exists {
		return nil, api.NewPoolNotFoundError(task.ID.String())
	}
	entry := v.(*poolEntry)

	// Add task.Context pairs to the frame context
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

// createPool creates a new pool for a function.
func (m *Manager) createPool(id registry.ID, cfg *api.FunctionConfig) error {
	compiled, err := m.code.Compile(id, functionBuildOptions())
	if err != nil {
		return api.NewCompileError(err)
	}

	// Create process factory
	factory := m.createFactory(compiled)

	// Determine pool config
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

	// Select pool type based on config
	var pool funcpool.Pool
	poolType := cfg.Pool.Type
	if poolType == "" {
		poolType = api.PoolTypeInline // Default to inline for max performance (no pooling)
	}

	maxWorkers := cfg.Pool.MaxSize
	if maxWorkers == 0 {
		maxWorkers = 16
	}

	// Create execution hooks for topology integration
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
		return api.NewUnknownPoolTypeError(poolType)
	}

	if err != nil {
		return api.NewCreatePoolError(err)
	}

	entry := &poolEntry{
		pool:   pool,
		config: cfg.Pool,
		method: cfg.Method,
	}
	m.pools.Store(id, entry)

	// Register pool as relay host using function ID
	if m.node != nil {
		if err := m.node.RegisterHost(id.String(), pool); err != nil {
			m.log.Warn("failed to register pool as host", zap.String("id", id.String()), zap.Error(err))
		}
	}

	m.mu.Lock()
	started := m.started
	m.mu.Unlock()

	if started {
		pool.Start()
	}

	return nil
}

// replacePool stops old pool and creates new one.
func (m *Manager) replacePool(id registry.ID, cfg *api.FunctionConfig) error {
	m.removePool(id)
	return m.createPool(id, cfg)
}

// removePool stops and removes a pool.
func (m *Manager) removePool(id registry.ID) {
	v, exists := m.pools.LoadAndDelete(id)
	if !exists {
		return
	}
	entry := v.(*poolEntry)
	entry.pool.Stop()

	// Unregister pool from relay
	if m.node != nil {
		m.node.UnregisterHost(id.String())
	}
}

// createFactory creates a ProcessFactory from compiled code.
func (m *Manager) createFactory(compiled *code.CompiledMain) funcpool.Factory {
	return func() (process.Process, error) {
		return createProcess(compiled)
	}
}

// createProcess creates a new process with standard bindings.
func createProcess(compiled *code.CompiledMain) (process.Process, error) {
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
		// Handle compiled proto dependencies (libraries)
		if dep.Proto != nil {
			proto := dep.Proto
			name := dep.Name
			binders = append(binders, func(L *lua.LState) {
				L.PreloadModule(name, func(L *lua.LState) int {
					fn := L.LoadProto(proto)
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
			proto := pre.Proto
			name := pre.Name
			binders = append(binders, func(L *lua.LState) {
				L.PreloadModule(name, func(L *lua.LState) int {
					fn := L.LoadProto(proto)
					L.Push(fn)
					L.Call(0, 1)
					return 1
				})
			})
		}
	}

	cfg := engine.FactoryConfig{
		Proto:         compiled.Main,
		ModuleBinders: binders,
	}

	factory := engine.NewFactory(cfg)
	return factory()
}

// functionBuildOptions returns build options for functions.
func functionBuildOptions() *code.BuildOptions {
	return code.NewBuildOptions().
		WithMode(code.AllowAll)
}

// registerCaller registers function in the function system.
func (m *Manager) registerCaller(ctx context.Context, id registry.ID, options runtime.Options) {
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
func (m *Manager) unregisterCaller(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Delete,
		Path:   id.String(),
	})
}

// createExecutionHooks creates execution hooks for topology integration.
// Functions are virtual processes - they don't need topology registration
// for simple request/response patterns. This eliminates lock contention.
func (m *Manager) createExecutionHooks() funcpool.ExecutionHooks {
	if m.topo == nil || m.pidReg == nil {
		return funcpool.ExecutionHooks{}
	}

	onStart := func(ctx context.Context, _ process.Process) { // todo: why do we pass process here? should we also pass pid so it's much faster?
		pid, ok := runtime.GetFramePID(ctx)
		if !ok || pid.String() == "" {
			return
		}

		// if err := m.topo.Register(pid); err != nil {
		//	m.log.Warn("failed to register function PID in topology",
		//		zap.String("pid", pid.String()),
		//		zap.Error(err))
		//}
	}

	onComplete := func(ctx context.Context, _ *runtime.Result) {
		pid, ok := runtime.GetFramePID(ctx)
		if !ok || pid.String() == "" {
			return
		}

		// if result.Error != nil {
		//	if errors.Is(result.Error, supervisor.ErrExit) {
		//		result.Error = nil
		//	}
		//}

		// m.topo.Notify(pid, result)
		// m.pidReg.Remove(pid)
		// m.topo.Remove(pid)
	}

	return funcpool.ExecutionHooks{
		OnStart:    onStart,
		OnComplete: onComplete,
	}
}
