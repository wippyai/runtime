package function

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/runtime/lua/component"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/code"
	queupool "github.com/ponyruntime/pony/runtime/lua/pool/queued"
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
		WithDenied(registry.ID{Name: "command"}).
		WithDenied(registry.ID{Name: "pubsub"}).
		WithDenied(registry.ID{Name: "process"}).
		WithPreloaded(code.Preload{Name: "channel", ModuleID: registry.ID{Name: "channel"}}).
		WithPreloaded(code.Preload{Name: "func", ModuleID: registry.ID{Name: "func"}})

	layers = component.WithLayerInitializer(func() []engine.RunnerOption {
		channels := channel.NewChannelLayer()
		return []engine.RunnerOption{
			engine.WithLayer(channels),
			engine.WithLayer(async.NewAsyncLayer(channels, 4096)),
			engine.WithLayer(coroutine.NewCoroutineLayer()),
		}
	})
}

// Manager handles Lua function compilation, pooling and execution
type Manager struct {
	log     *zap.Logger
	code    *code.Manager
	bus     events.Bus
	vms     sync.Map // map[registry.ID]api.Callable
	configs sync.Map // map[registry.ID]*api.FunctionConfig
}

// NewManager creates a new function manager instance
func NewManager(log *zap.Logger, code *code.Manager, bus events.Bus) *Manager {
	return &Manager{
		log:  log,
		code: code,
		bus:  bus,
	}
}

// pushHandler creates or updates a pool for a function
func (m *Manager) pushHandler(id registry.ID, cfg *api.FunctionConfig) error {
	// Compile function using code manager
	compiled, err := m.code.Compile(id, functionBuild)
	if err != nil {
		return fmt.Errorf("failed to compile function: %w", err)
	}

	// Spawn new pool
	pool, err := m.createVM(cfg, compiled)
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
	if err := m.code.AddNode(ctx, node, component.BuildImports(cfg.Import, cfg.Modules)); err != nil {
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
	if err := m.code.UpdateNode(ctx, node, component.BuildImports(cfg.Import, cfg.Modules)); err != nil {
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
		if closer, ok := pool.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				m.log.Error("failed to close function", zap.Error(err))
			}
		}
	}

	// Remove config
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
func (m *Manager) getHandler(handler registry.ID) (string, api.VM, error) {
	vmInterface, ok := m.vms.Load(handler)
	if !ok {
		return "", nil, fmt.Errorf("no function found for function: %s", handler)
	}

	cfgInterface, ok := m.configs.Load(handler)
	if !ok {
		return "", nil, fmt.Errorf("no config found for function: %s", handler)
	}

	return cfgInterface.(*api.FunctionConfig).Method, vmInterface.(api.VM), nil
}

// Execute runs a function with given arguments
func (m *Manager) Execute(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
	// generally speaking we can offload cold handlers
	method, vm, err := m.getHandler(task.Handler)
	if err != nil {
		return nil, err
	}

	// Spawn result channel
	resultChan := make(chan *runtime.Result, 1)

	// Execute in goroutine to handle async results
	go func() {
		defer close(resultChan)

		// Get transcoder from context
		dtt := payload.GetTranscoder(ctx)
		if dtt == nil {
			resultChan <- &runtime.Result{Error: fmt.Errorf("no transcoder found in context")}
			return
		}

		// Convert payloads to Lua values
		args := make([]lua.LValue, len(task.Payloads))
		for i, p := range task.Payloads {
			// Transcode to Lua format if needed
			luaPayload, err := dtt.Transcode(p, payload.Lua)
			if err != nil {
				resultChan <- &runtime.Result{
					Error: fmt.Errorf("failed to transcode payload: %w", err),
				}
				return
			}
			args[i] = luaPayload.Data().(lua.LValue)
		}

		result, err := vm.Execute(ctx, method, args...)
		if err != nil {
			m.log.Error("failed to execute function", zap.Error(err))
		}
		resultChan <- &runtime.Result{
			Payload: payload.NewPayload(result, payload.Lua),
			Error:   err,
		}
	}()

	return resultChan, nil
}

// createVM creates a new pool based on config and compiled code
func (m *Manager) createVM(cfg *api.FunctionConfig, compiled *code.CompiledMain) (api.VM, error) {
	fvm, err := component.NewRunnerFactory(m.log, compiled, layers)
	if err != nil {
		return nil, fmt.Errorf("failed to compile: %w", err)
	}

	if err := fvm.Compile(); err != nil {
		return nil, fmt.Errorf("failed to compile: %w", err)
	}

	if cfg.Pool.Workers > 0 {
		return queupool.NewPool(
			fvm,
			queupool.WithSize(cfg.Pool.Size),
			queupool.WithLogger(m.log),
			queupool.WithWorkers(cfg.Pool.Workers),
		)
	}

	return syncpool.NewPool(
		fvm,
		syncpool.WithSize(cfg.Pool.Size),
		syncpool.WithLogger(m.log),
	)
}

// registerCaller registers function in the function system
func (m *Manager) registerCaller(ctx context.Context, id registry.ID, method string) {
	m.bus.Send(ctx, events.Event{
		System: function.System,
		Kind:   function.RegisterFunctionHandler,
		Path:   id.String(),
		Data:   function.Func(m.Execute),
	})
}

// unregisterCaller removes function from the function system
func (m *Manager) unregisterCaller(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, events.Event{
		System: function.System,
		Kind:   function.DeleteFunctionHandler,
		Path:   id.String(),
	})
}
