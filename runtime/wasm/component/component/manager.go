// Package component provides management for WebAssembly Component Model functions.
package component

import (
	"context"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/api/topology"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	"github.com/wippyai/runtime/runtime/wasm/engine"
	"github.com/wippyai/runtime/runtime/wasm/host"
	funcpool "github.com/wippyai/runtime/system/scheduler/pool"
	"github.com/wippyai/runtime/system/scheduler/pool/inline"
	"github.com/wippyai/runtime/system/scheduler/pool/lazy"
	"github.com/wippyai/runtime/system/scheduler/pool/static"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

// configEntry holds config for a Component Model function.
type configEntry struct {
	method    string
	transport string
	pool      wasmapi.PoolConfig
	config    *wasmapi.ComponentFunctionConfig
}

// poolEntry wraps a pool with its config.
type poolEntry struct {
	pool   funcpool.Pool
	method string
}

// Manager handles Component Model function loading, pooling and execution.
type Manager struct {
	ctx        context.Context
	log        *zap.Logger
	bus        event.Bus
	dispatcher dispatcher.Dispatcher
	fsRegistry fsapi.Registry
	runtime    *wasmrt.Runtime
	hosts      *host.Registry
	topo       topology.Topology
	pidReg     topology.PIDRegistry
	node       relay.Node

	mu      sync.RWMutex
	pools   map[registry.ID]*poolEntry
	configs map[registry.ID]*configEntry
	started bool
}

// NewManager creates a new Component Model function manager.
func NewManager(
	log *zap.Logger,
	bus event.Bus,
	disp dispatcher.Dispatcher,
	fsRegistry fsapi.Registry,
	runtime *wasmrt.Runtime,
	hosts *host.Registry,
) *Manager {
	return &Manager{
		log:        log,
		bus:        bus,
		dispatcher: disp,
		fsRegistry: fsRegistry,
		runtime:    runtime,
		hosts:      hosts,
		pools:      make(map[registry.ID]*poolEntry),
		configs:    make(map[registry.ID]*configEntry),
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

	m.log.Info("component function manager started")
	return nil
}

// Stop stops all pools gracefully.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, entry := range m.pools {
		entry.pool.Stop()
		if m.node != nil {
			m.node.UnregisterHost(id.String())
		}
		m.log.Debug("pool stopped", zap.String("id", id.String()))
	}
	m.pools = make(map[registry.ID]*poolEntry)
	m.started = false

	m.log.Info("component function manager stopped")
}

// Add is called when a new entry is created.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != wasmapi.FunctionComponent {
		return runtimewasm.NewInvalidEntryKindError(entry.Kind, wasmapi.FunctionComponent)
	}

	cfg, err := wasmcomponent.UnpackConfig[wasmapi.ComponentFunctionConfig](ctx, entry)
	if err != nil {
		return runtimewasm.NewUnpackConfigError("component function", err)
	}

	configEntry := &configEntry{
		method:    cfg.Method,
		transport: cfg.Transport,
		pool:      cfg.Pool,
		config:    cfg,
	}

	if err := m.createPool(ctx, entry.ID, configEntry); err != nil {
		return runtimewasm.NewCreatePoolError(err)
	}

	m.storeConfig(entry.ID, configEntry)

	opts, _ := cfg.Meta.GetBag("options")
	if err := m.registerCaller(ctx, entry.ID, opts); err != nil {
		m.removePool(entry.ID)
		m.deleteConfig(entry.ID)
		return err
	}

	m.log.Debug("component function added",
		zap.String("id", entry.ID.String()),
		zap.String("fs", cfg.FS),
		zap.String("path", cfg.Path),
	)
	return nil
}

// Update is called when an existing entry is updated.
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != wasmapi.FunctionComponent {
		return runtimewasm.NewInvalidEntryKindError(entry.Kind, wasmapi.FunctionComponent)
	}

	cfg, err := wasmcomponent.UnpackConfig[wasmapi.ComponentFunctionConfig](ctx, entry)
	if err != nil {
		return runtimewasm.NewUnpackConfigError("component function", err)
	}

	configEntry := &configEntry{
		method:    cfg.Method,
		transport: cfg.Transport,
		pool:      cfg.Pool,
		config:    cfg,
	}

	if err := m.replacePool(ctx, entry.ID, configEntry); err != nil {
		return runtimewasm.NewReplacePoolError(err)
	}

	m.storeConfig(entry.ID, configEntry)

	opts, _ := cfg.Meta.GetBag("options")
	if err := m.registerCaller(ctx, entry.ID, opts); err != nil {
		return err
	}

	m.log.Debug("component function updated", zap.String("id", entry.ID.String()))
	return nil
}

// Delete is called when an entry is removed.
func (m *Manager) Delete(_ context.Context, entry registry.Entry) error {
	if entry.Kind != wasmapi.FunctionComponent {
		return runtimewasm.NewInvalidEntryKindError(entry.Kind, wasmapi.FunctionComponent)
	}

	m.removePool(entry.ID)
	m.deleteConfig(entry.ID)
	m.unregisterCaller(entry.ID)

	m.log.Debug("component function deleted", zap.String("id", entry.ID.String()))
	return nil
}

// Execute runs a function with given task.
func (m *Manager) Execute(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	m.mu.RLock()
	entry, exists := m.pools[task.ID]
	m.mu.RUnlock()

	if !exists {
		return nil, runtimewasm.NewPoolNotFoundError(task.ID.String())
	}

	return entry.pool.Call(ctx, entry.method, task.Payloads)
}

// createPool creates a new pool for a function.
func (m *Manager) createPool(ctx context.Context, id registry.ID, cfg *configEntry) error {
	wasmBytes, err := wasmcomponent.LoadAndVerifyWASM(m.fsRegistry, cfg.config.FS, cfg.config.Path, cfg.config.Hash)
	if err != nil {
		return err
	}

	module, err := engine.LoadComponent(ctx, m.runtime, wasmBytes)
	if err != nil {
		return err
	}

	var transport wasmapi.Transport
	if cfg.transport != "" {
		// Transport implementation would be resolved here
		// For now, nil transport means use default payload conversion
	}

	factory := engine.NewFactoryWithTransport(m.runtime, module, transport)
	factoryFn := factory.CreateWithContext(ctx)

	execHooks := m.createExecutionHooks()
	var pool funcpool.Pool

	if cfg.pool.Type != "" {
		pool, err = m.createPoolByType(cfg.pool.Type, factoryFn, cfg.pool, execHooks)
	} else {
		pool, err = m.autoSelectPool(factoryFn, cfg.pool, execHooks)
	}

	if err != nil {
		return err
	}

	m.mu.Lock()
	m.pools[id] = &poolEntry{
		pool:   pool,
		method: cfg.method,
	}
	started := m.started
	m.mu.Unlock()

	if m.node != nil {
		if err := m.node.RegisterHost(id.String(), pool); err != nil {
			m.log.Warn("failed to register pool as host", zap.String("id", id.String()), zap.Error(err))
		}
	}

	if started {
		pool.Start()
	}

	return nil
}

// replacePool stops old pool and creates new one.
func (m *Manager) replacePool(ctx context.Context, id registry.ID, cfg *configEntry) error {
	m.removePool(id)
	return m.createPool(ctx, id, cfg)
}

// removePool stops and removes a pool.
func (m *Manager) removePool(id registry.ID) {
	m.mu.Lock()
	entry, exists := m.pools[id]
	if exists {
		delete(m.pools, id)
	}
	m.mu.Unlock()

	if exists {
		entry.pool.Stop()
		if m.node != nil {
			m.node.UnregisterHost(id.String())
		}
	}
}

// autoSelectPool automatically selects pool type based on config.
func (m *Manager) autoSelectPool(factory process.FactoryFunc, cfg wasmapi.PoolConfig, hooks funcpool.ExecutionHooks) (funcpool.Pool, error) {
	if cfg.Size == 0 {
		return inline.New(factory, m.dispatcher, hooks)
	}

	queueSize := cfg.Buffer
	if queueSize == 0 {
		queueSize = cfg.Size * 64
	}
	return static.New(factory, m.dispatcher, static.Config{
		Workers:   cfg.Size,
		QueueSize: queueSize,
	}, hooks)
}

// createPoolByType creates a pool of the specified type.
func (m *Manager) createPoolByType(poolType string, factory process.FactoryFunc, cfg wasmapi.PoolConfig, hooks funcpool.ExecutionHooks) (funcpool.Pool, error) {
	switch poolType {
	case wasmapi.PoolTypeInline:
		return inline.New(factory, m.dispatcher, hooks)

	case wasmapi.PoolTypeLazy:
		maxWorkers := cfg.Size
		if maxWorkers == 0 {
			maxWorkers = 16
		}
		return lazy.New(factory, m.dispatcher, lazy.Config{
			MaxWorkers:  maxWorkers,
			IdleTimeout: 30 * time.Second,
		}, hooks)

	case wasmapi.PoolTypeStatic:
		workers := cfg.Size
		if workers == 0 {
			workers = 8
		}
		queueSize := cfg.Buffer
		if queueSize == 0 {
			queueSize = workers * 64
		}
		return static.New(factory, m.dispatcher, static.Config{
			Workers:   workers,
			QueueSize: queueSize,
		}, hooks)

	default:
		return nil, runtimewasm.NewUnknownPoolTypeError(poolType)
	}
}

// registerCaller registers function in the function system.
func (m *Manager) registerCaller(ctx context.Context, id registry.ID, options runtime.Options) error {
	path := id.String()

	awaitSvc := event.GetAwaitService(ctx)
	if awaitSvc == nil {
		return runtimewasm.NewRegisterCallerError(&id, nil)
	}

	m.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.FunctionRegister,
		Path:   path,
		Data: &function.FuncEntry{
			Handler: m.Execute,
			Options: options,
		},
	})

	result := awaitSvc.Await(ctx, function.System, "function.(accept|reject)", path, 30*time.Second)
	if !result.Accepted {
		return runtimewasm.NewRegisterCallerError(&id, result.Error)
	}
	return nil
}

// unregisterCaller removes function from the function system.
func (m *Manager) unregisterCaller(id registry.ID) {
	m.bus.Send(m.ctx, event.Event{
		System: function.System,
		Kind:   function.FunctionDelete,
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

		m.pidReg.Remove(pid)
		m.topo.Complete(pid, result)
	}

	return funcpool.ExecutionHooks{
		OnStart:    onStart,
		OnComplete: onComplete,
	}
}

// storeConfig stores a config entry.
func (m *Manager) storeConfig(id registry.ID, cfg *configEntry) {
	m.mu.Lock()
	m.configs[id] = cfg
	m.mu.Unlock()
}

// deleteConfig removes a config entry.
func (m *Manager) deleteConfig(id registry.ID) {
	m.mu.Lock()
	delete(m.configs, id)
	m.mu.Unlock()
}

// Compile-time check
var _ registry.EntryListener = (*Manager)(nil)
