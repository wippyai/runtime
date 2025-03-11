package function

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/code"
	"github.com/ponyruntime/pony/runtime/lua/component"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	"github.com/ponyruntime/pony/runtime/lua/pool/flex"
	"github.com/ponyruntime/pony/runtime/lua/pool/queued"
	syncpool "github.com/ponyruntime/pony/runtime/lua/pool/sync"
	"go.uber.org/zap"
)

var (
	functionBuild *code.BuildOptions
	layers        component.Option
)

func init() {
	functionBuild = code.NewBuildOptions().
		WithMode(code.AllowAll).
		WithPreloaded(code.Preload{Name: "channel", ModuleID: registry.ID{Name: "channel"}}).
		WithPreloaded(code.Preload{Name: "process", ModuleID: registry.ID{Name: "process"}}).
		WithPreloaded(code.Preload{Name: "function_api", ModuleID: registry.ID{Name: "function_api"}}).
		WithPreloaded(code.Preload{Name: "os", ModuleID: registry.ID{Name: "ostime"}}).
		WithPreloaded(code.Preload{Name: "payload", ModuleID: registry.ID{Name: "payload"}})

	layers = component.WithRunnerOption(
		engine.WithLayer(channel.NewChannelLayer()),
		engine.WithLayer(subscribe.NewSubscribeLayer()),
		engine.WithLayer(coroutine.NewCoroutineLayer()),
	)
}

type pool interface {
	Execute(ctx context.Context, task runtime.Task) (chan *runtime.Result, error)
	Close()
}

// Manager handles Lua function compilation, pooling and execution
type Manager struct {
	log     *zap.Logger
	code    *code.Manager
	bus     event.Bus
	vms     sync.Map // map[registry.Source]api.Callable
	configs sync.Map // map[registry.Source]*api.FunctionConfig
}

// NewManager creates a new function manager instance
func NewManager(log *zap.Logger, code *code.Manager, bus event.Bus) *Manager {
	return &Manager{
		log:  log,
		code: code,
		bus:  bus,
	}
}

// pushHandler creates or updates a pool for a function
func (m *Manager) pushHandler(id registry.ID, cfg *api.FunctionConfig) error {
	// Spawn new pool without immediately compiling
	pool, err := m.createPool(id, cfg)
	if err != nil {
		return fmt.Errorf("failed to create pool: %w", err)
	}

	oldPool, exists := m.vms.Load(id)

	// Store new pool and config
	m.vms.Store(id, pool)
	m.configs.Store(id, cfg)

	// Close old pool if it exists
	if exists {
		if closer, ok := oldPool.(api.VM); ok {
			closer.Close()
		}
	}

	return nil
}

// Add creates and registers a new function
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindFunction {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindFunction)
	}

	// Unpack config
	cfg, err := component.UnpackConfig[api.FunctionConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack function config: %w", err)
	}

	// Spawn node
	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindFunction,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	// Add to code manager
	if err := m.code.AddNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules)); err != nil {
		return fmt.Errorf("failed to add function: %w", err)
	}

	// Spawn and store pool
	if err := m.pushHandler(entry.ID, cfg); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		return fmt.Errorf("failed to create function: %w", err)
	}

	// Register function caller
	m.registerCaller(ctx, entry.ID, cfg.Method)

	return nil
}

// Update updates an existing function
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindFunction {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindFunction)
	}

	// Unpack config
	cfg, err := component.UnpackConfig[api.FunctionConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack function config: %w", err)
	}

	// Spawn node
	node := code.Node{
		ID:     entry.ID,
		Kind:   api.KindFunction,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	// Update in code manager
	if err := m.code.UpdateNode(ctx, node, component.BuildImports(cfg.Imports, cfg.Modules)); err != nil {
		return fmt.Errorf("failed to update function node: %w", err)
	}

	// Update pool
	if err := m.pushHandler(entry.ID, cfg); err != nil {
		return fmt.Errorf("failed to refresh function: %w", err)
	}

	return nil
}

// Delete removes a function
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindFunction {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindFunction)
	}

	// Delete from code manager
	if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
		return fmt.Errorf("failed to delete function node: %w", err)
	}

	// Close and remove pool
	if pool, ok := m.vms.LoadAndDelete(entry.ID); ok {
		if closer, ok := pool.(api.VM); ok {
			closer.Close()
		}
	}

	// Done config
	m.configs.Delete(entry.ID)

	// Unregister function caller
	m.unregisterCaller(ctx, entry.ID)

	return nil
}

// Invalidate handles invalidation of functions
func (m *Manager) Invalidate(ctx context.Context, ids []registry.ID) {
	for _, id := range ids {
		m.log.Debug("invalidating function", zap.String("id", id.String()))

		// Get current config
		cfgAny, exists := m.configs.Load(id)
		if !exists {
			continue
		}
		cfg := cfgAny.(*api.FunctionConfig)

		// Refresh pool with existing config
		if err := m.pushHandler(id, cfg); err != nil {
			m.log.Error("failed to refresh function", zap.Error(err))
		}
	}
}

// getHandler retrieves the method name and VM for a given handler
func (m *Manager) getHandler(handler registry.ID) (pool, error) {
	vmInterface, ok := m.vms.Load(handler)
	if !ok {
		return nil, fmt.Errorf("no function found for function: %s", handler)
	}

	return vmInterface.(pool), nil
}

// Execute runs a function with given arguments
func (m *Manager) Execute(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
	// Get handler directly - it already implements our pool interface
	vm, err := m.getHandler(task.ID)
	if err != nil {
		return nil, err
	}

	// Use the pool to execute the task directly
	return vm.Execute(ctx, task)
}

// createPool creates a new pool based on config
func (m *Manager) createPool(id registry.ID, cfg *api.FunctionConfig) (pool, error) {
	// Convert imports for code manager
	imports := component.BuildImports(cfg.Imports, cfg.Modules)

	// Flex pool case - no workers, either no size or has max size
	isFlexPool := cfg.Pool.Workers == 0 && (cfg.Pool.Size == 0 || cfg.Pool.MaxSize > 0)

	// For flex pool with WarmStart=false, use lazy factory
	if isFlexPool && !cfg.Pool.WarmStart {
		// Create a lazy factory
		factory := NewCompilerFactory(
			m.log,
			m.code,
			id,
			functionBuild,
			imports,
			layers,
		)

		// Create flex pool with the lazy factory
		maxSize := cfg.Pool.MaxSize
		if maxSize <= 0 {
			maxSize = api.DefaultMaxSize // Use default if not specified
		}

		return flex.NewTaskPool(
			factory,
			cfg.Method,
			flex.WithTaskMaxSize(maxSize),
			flex.WithTaskLogger(m.log),
		)
	}

	// For all other cases, compile immediately
	compiled, err := m.code.Compile(id, functionBuild)
	if err != nil {
		return nil, fmt.Errorf("failed to compile function: %w", err)
	}

	// Create a real factory
	factory, err := component.NewRunnerFactory(m.log, compiled, layers)
	if err != nil {
		return nil, fmt.Errorf("failed to create factory: %w", err)
	}

	// Compile the factory
	if err := factory.Compile(); err != nil {
		return nil, fmt.Errorf("failed to compile: %w", err)
	}

	// For flex pool with WarmStart=true, use the compiled factory with flex pool
	if isFlexPool {
		maxSize := cfg.Pool.MaxSize
		if maxSize <= 0 {
			maxSize = api.DefaultMaxSize // Use default if not specified
		}

		return flex.NewTaskPool(
			factory,
			cfg.Method,
			flex.WithTaskMaxSize(maxSize),
			flex.WithTaskLogger(m.log),
		)
	}

	// For worker-based execution, use queued pool
	if cfg.Pool.Workers > 0 {
		return queued.NewTaskPool(
			factory,
			cfg.Method,
			queued.WithTaskSize(cfg.Pool.Size),
			queued.WithTaskLogger(m.log),
			queued.WithTaskWorkers(cfg.Pool.Workers),
		)
	}

	// For synchronous execution, use sync pool
	return syncpool.NewTaskPool(
		factory,
		cfg.Method,
		syncpool.WithTaskPoolSize(cfg.Pool.Size),
		syncpool.WithTaskPoolLogger(m.log),
	)
}

// registerCaller registers function in the function system
func (m *Manager) registerCaller(ctx context.Context, id registry.ID, method string) {
	m.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   id.String(),
		Data:   function.Func(m.Execute),
	})
}

// unregisterCaller removes function from the function system
func (m *Manager) unregisterCaller(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Delete,
		Path:   id.String(),
	})
}
