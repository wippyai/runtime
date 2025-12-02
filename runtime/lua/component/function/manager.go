// Package function provides Lua function management.
// Uses pluggable pool schedulers for different workload patterns.
package function

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/component"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/modules/http"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	"github.com/wippyai/runtime/runtime/lua/modules/stream"
	timeyields "github.com/wippyai/runtime/runtime/lua/modules/time"
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

	mu      sync.RWMutex
	pools   map[registry.ID]*poolEntry
	configs sync.Map
	started bool
}

// NewManager creates a new function manager.
func NewManager(log *zap.Logger, code *code.Manager, bus event.Bus, disp dispatcher.Dispatcher) *Manager {
	return &Manager{
		log:        log.Named("func"),
		code:       code,
		bus:        bus,
		dispatcher: disp,
		pools:      make(map[registry.ID]*poolEntry),
	}
}

// Start marks the manager as ready to accept pools.
func (m *Manager) Start(ctx context.Context) error {
	m.ctx = ctx
	m.topo = topology.GetTopology(ctx)
	m.pidReg = topology.GetRegistry(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
	m.log.Info("function manager started")
	return nil
}

// Stop stops all pools gracefully.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, entry := range m.pools {
		entry.pool.Stop()
		m.log.Debug("pool stopped", zap.String("id", id.String()))
	}
	m.pools = make(map[registry.ID]*poolEntry)
	m.started = false
	m.log.Info("function manager stopped")
}

// Add creates and registers a new function.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindFunction {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindFunction)
	}

	cfg, err := component.UnpackConfig[api.FunctionConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack function config: %w", err)
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
		return fmt.Errorf("failed to add function: %w", err)
	}

	// Create pool
	if err := m.createPool(entry.ID, cfg); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		return fmt.Errorf("failed to create pool: %w", err)
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
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindFunction)
	}

	cfg, err := component.UnpackConfig[api.FunctionConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack function config: %w", err)
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
		return fmt.Errorf("failed to update function node: %w", err)
	}

	// Replace pool
	if err := m.replacePool(entry.ID, cfg); err != nil {
		return fmt.Errorf("failed to replace pool: %w", err)
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
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindFunction)
	}

	// Delete from code manager
	if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
		return fmt.Errorf("failed to delete function node: %w", err)
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
	m.mu.RLock()
	entry, exists := m.pools[task.ID]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("pool not found: %s", task.ID)
	}

	// Add task.Context pairs to the frame context
	if len(task.Context) > 0 {
		fc := ctxapi.FrameFromContext(ctx)
		if fc != nil {
			for _, pair := range task.Context {
				_ = fc.Set(pair.Key, pair.Value)
			}
		}
	}

	return entry.pool.Call(ctx, entry.method, task.Payloads)
}

// createPool creates a new pool for a function.
func (m *Manager) createPool(id registry.ID, cfg *api.FunctionConfig) error {
	// Compile the function
	compiled, err := m.code.Compile(id, functionBuildOptions())
	if err != nil {
		return fmt.Errorf("failed to compile: %w", err)
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
		return fmt.Errorf("unknown pool type: %s", poolType)
	}

	if err != nil {
		return fmt.Errorf("failed to create pool: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.pools[id] = &poolEntry{
		pool:   pool,
		config: cfg.Pool,
		method: cfg.Method,
	}

	if m.started {
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
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, exists := m.pools[id]; exists {
		entry.pool.Stop()
		delete(m.pools, id)
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
	binders = append(binders, stream.BindStream, http.Bind, timeyields.BindYields, processmod.BindGlobal)

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
					L.Call(1, 1)
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
					L.Call(1, 1)
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
func (m *Manager) createExecutionHooks() funcpool.ExecutionHooks {
	if m.topo == nil || m.pidReg == nil {
		return funcpool.ExecutionHooks{}
	}

	onStart := func(ctx context.Context, _ process.Process) {
		pid, ok := runtime.GetFramePID(ctx)
		if !ok || pid.String() == "" {
			return
		}

		if err := m.topo.Register(pid); err != nil {
			m.log.Warn("failed to register function PID in topology",
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
			if errors.Is(result.Error, supervisor.ErrExit) {
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
