// Package function provides WASM function management for wippy.
package function

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/runtime/wasm/engine"
	"github.com/wippyai/runtime/runtime/wasm/host/clock"
	funcpool "github.com/wippyai/runtime/system/scheduler/pool"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

// poolEntry wraps a pool with its config.
type poolEntry struct {
	pool   funcpool.Pool
	config api.FunctionConfig
	module *wasmrt.Module
}

// Manager handles WASM function compilation, pooling and execution.
type Manager struct {
	log        *zap.Logger
	bus        event.Bus
	dispatcher dispatcher.Dispatcher
	runtime    *wasmrt.Runtime

	mu      sync.RWMutex
	pools   map[registry.ID]*poolEntry
	configs sync.Map
	started bool
}

// NewManager creates a new WASM function manager.
func NewManager(log *zap.Logger, bus event.Bus, disp dispatcher.Dispatcher) *Manager {
	return &Manager{
		log:        log.Named("wasm"),
		bus:        bus,
		dispatcher: disp,
		pools:      make(map[registry.ID]*poolEntry),
	}
}

// Start initializes the WASM runtime and marks manager ready.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rt, err := wasmrt.New(ctx)
	if err != nil {
		return fmt.Errorf("create WASM runtime: %w", err)
	}

	// Register clock host
	if err := rt.RegisterHost(clock.New()); err != nil {
		rt.Close(ctx)
		return fmt.Errorf("register clock host: %w", err)
	}

	m.runtime = rt
	m.started = true
	m.log.Info("WASM function manager started")
	return nil
}

// Stop stops all pools and closes the runtime.
func (m *Manager) Stop(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, entry := range m.pools {
		entry.pool.Stop()
		m.log.Debug("pool stopped", zap.String("id", id.String()))
	}
	m.pools = make(map[registry.ID]*poolEntry)

	if m.runtime != nil {
		m.runtime.Close(ctx)
		m.runtime = nil
	}

	m.started = false
	m.log.Info("WASM function manager stopped")
}

// Add creates and registers a new WASM function.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindFunction {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindFunction)
	}

	cfg, err := unpackConfig(ctx, entry)
	if err != nil {
		return fmt.Errorf("unpack config: %w", err)
	}

	// Compile WAT to WASM module
	module, err := engine.CompileWAT(ctx, m.runtime, cfg.Source, cfg.Wit)
	if err != nil {
		return fmt.Errorf("compile WAT: %w", err)
	}

	// Create pool
	if err := m.createPool(entry.ID, cfg, module); err != nil {
		return fmt.Errorf("create pool: %w", err)
	}

	// Store config
	m.configs.Store(entry.ID, cfg)

	// Register function caller
	m.registerCaller(ctx, entry.ID, cfg.Method)

	m.log.Info("WASM function added",
		zap.String("id", entry.ID.String()),
		zap.String("method", cfg.Method),
	)

	return nil
}

// Update updates an existing WASM function.
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindFunction {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindFunction)
	}

	cfg, err := unpackConfig(ctx, entry)
	if err != nil {
		return fmt.Errorf("unpack config: %w", err)
	}

	// Compile new WAT
	module, err := engine.CompileWAT(ctx, m.runtime, cfg.Source, cfg.Wit)
	if err != nil {
		return fmt.Errorf("compile WAT: %w", err)
	}

	// Replace pool
	if err := m.replacePool(entry.ID, cfg, module); err != nil {
		return fmt.Errorf("replace pool: %w", err)
	}

	// Update config
	m.configs.Store(entry.ID, cfg)

	// Re-register caller
	m.registerCaller(ctx, entry.ID, cfg.Method)

	m.log.Info("WASM function updated", zap.String("id", entry.ID.String()))
	return nil
}

// Delete removes a WASM function.
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindFunction {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindFunction)
	}

	// Stop and remove pool
	m.removePool(entry.ID)

	// Remove config
	m.configs.Delete(entry.ID)

	// Unregister caller
	m.unregisterCaller(ctx, entry.ID)

	m.log.Info("WASM function deleted", zap.String("id", entry.ID.String()))
	return nil
}

// Execute runs a WASM function by ID.
func (m *Manager) Execute(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	m.mu.RLock()
	entry, exists := m.pools[task.ID]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("function %s not found", task.ID)
	}

	return entry.pool.Call(ctx, entry.config.Method, task.Payloads)
}

// createPool creates a new pool for a WASM function.
func (m *Manager) createPool(id registry.ID, cfg *api.FunctionConfig, module *wasmrt.Module) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return fmt.Errorf("manager not started")
	}

	factory := engine.NewFactory(m.runtime, module)

	p, err := funcpool.NewInline(factory.Create(), m.dispatcher)
	if err != nil {
		return fmt.Errorf("create pool: %w", err)
	}

	m.pools[id] = &poolEntry{
		pool:   p,
		config: *cfg,
		module: module,
	}

	return nil
}

// replacePool replaces an existing pool.
func (m *Manager) replacePool(id registry.ID, cfg *api.FunctionConfig, module *wasmrt.Module) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, exists := m.pools[id]; exists {
		entry.pool.Stop()
	}

	factory := engine.NewFactory(m.runtime, module)

	p, err := funcpool.NewInline(factory.Create(), m.dispatcher)
	if err != nil {
		return fmt.Errorf("create pool: %w", err)
	}

	m.pools[id] = &poolEntry{
		pool:   p,
		config: *cfg,
		module: module,
	}

	return nil
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

// registerCaller registers function in the function system via event bus.
func (m *Manager) registerCaller(ctx context.Context, id registry.ID, method string) {
	m.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   id.String(),
		Data: &function.FuncEntry{
			Handler: m.Execute,
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

// unpackConfig extracts FunctionConfig from a registry entry.
func unpackConfig(ctx context.Context, entry registry.Entry) (*api.FunctionConfig, error) {
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		return nil, fmt.Errorf("transcoder not found in context")
	}

	cfg := &api.FunctionConfig{}
	if err := dtt.Unmarshal(entry.Data, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Compile-time check
var _ registry.EntryListener = (*Manager)(nil)
