// Package component provides WASM component management for precompiled .wasm files.
package component

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/runtime/wasm/engine"
	"github.com/wippyai/runtime/runtime/wasm/host/clock"
	funcpool "github.com/wippyai/runtime/system/scheduler/pool"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
	wasihttp "github.com/wippyai/wasm-runtime/wasi/preview2/http"
	"go.uber.org/zap"
)

// poolEntry wraps a pool with its config.
type poolEntry struct {
	pool   funcpool.Pool
	config api.ComponentFunctionConfig
	module *wasmrt.Module
}

// Config holds configuration for the WASM component manager.
type Config struct {
	// StrictMode fails on missing imports instead of creating stubs.
	// Default is true.
	StrictMode bool
}

// DefaultConfig returns the default manager configuration.
func DefaultConfig() Config {
	return Config{
		StrictMode: true,
	}
}

// Manager handles precompiled WASM component loading, pooling and execution.
type Manager struct {
	log        *zap.Logger
	bus        event.Bus
	dispatcher dispatcher.Dispatcher
	fsRegistry fsapi.Registry
	transports api.TransportRegistry
	runtime    *wasmrt.Runtime
	config     Config

	mu      sync.RWMutex
	pools   map[registry.ID]*poolEntry
	configs sync.Map
	started bool
}

// NewManager creates a new WASM component manager with default config (strict mode enabled).
func NewManager(log *zap.Logger, bus event.Bus, disp dispatcher.Dispatcher, fsReg fsapi.Registry, transports api.TransportRegistry) *Manager {
	return NewManagerWithConfig(log, bus, disp, fsReg, transports, DefaultConfig())
}

// NewManagerWithConfig creates a new WASM component manager with custom config.
func NewManagerWithConfig(log *zap.Logger, bus event.Bus, disp dispatcher.Dispatcher, fsReg fsapi.Registry, transports api.TransportRegistry, cfg Config) *Manager {
	return &Manager{
		log:        log,
		bus:        bus,
		dispatcher: disp,
		fsRegistry: fsReg,
		transports: transports,
		config:     cfg,
		pools:      make(map[registry.ID]*poolEntry),
	}
}

// Start initializes the WASM runtime.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rt, err := wasmrt.NewWithConfig(ctx, wasmrt.Config{
		StrictMode: m.config.StrictMode,
	})
	if err != nil {
		return NewRuntimeError(err)
	}

	// Register wippy clock host
	if err := rt.RegisterHost(clock.New()); err != nil {
		rt.Close(ctx)
		return NewRegisterHostError(err)
	}

	// Register WASI Preview2 hosts
	wasi := preview2.New()
	if err := rt.RegisterWASI(wasi); err != nil {
		rt.Close(ctx)
		return NewRegisterHostError(err)
	}

	// Register HTTP types host
	httpHost := wasihttp.NewTypesHost(wasi.Resources())
	if err := rt.RegisterHost(httpHost); err != nil {
		rt.Close(ctx)
		return NewRegisterHostError(err)
	}

	m.runtime = rt
	m.started = true
	m.log.Info("WASM component manager started", zap.Bool("strict_mode", m.config.StrictMode))
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
	m.log.Info("WASM component manager stopped")
}

// Add loads and registers a new WASM component.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindComponentFunction {
		return NewInvalidEntryKindError(string(entry.Kind), string(api.KindComponentFunction))
	}

	cfg, err := unpackConfig(ctx, entry)
	if err != nil {
		return NewUnpackConfigError(err)
	}

	// Load WASM bytes from filesystem
	wasmBytes, err := m.loadWASM(cfg.FS, cfg.Path)
	if err != nil {
		return NewLoadWASMError(err)
	}

	// Verify hash
	if err := verifyHash(wasmBytes, cfg.Hash); err != nil {
		return NewHashVerificationError(err)
	}

	// Load as component (WIT is embedded in component binary)
	module, err := m.runtime.LoadComponent(ctx, wasmBytes)
	if err != nil {
		return NewLoadComponentError(err)
	}

	// Pre-compile at registration time to fail fast on missing imports
	if err := module.Compile(ctx); err != nil {
		return NewCompileError(err)
	}

	// Create pool
	if err := m.createPool(entry.ID, cfg, module); err != nil {
		return NewCreatePoolError(err)
	}

	// Store config
	m.configs.Store(entry.ID, cfg)

	// Register function caller
	m.registerCaller(ctx, entry.ID, cfg.Method)

	m.log.Info("WASM component added",
		zap.String("id", entry.ID.String()),
		zap.String("method", cfg.Method),
		zap.String("fs", cfg.FS),
		zap.String("path", cfg.Path),
	)

	return nil
}

// Update updates an existing WASM component.
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindComponentFunction {
		return NewInvalidEntryKindError(string(entry.Kind), string(api.KindComponentFunction))
	}

	cfg, err := unpackConfig(ctx, entry)
	if err != nil {
		return NewUnpackConfigError(err)
	}

	wasmBytes, err := m.loadWASM(cfg.FS, cfg.Path)
	if err != nil {
		return NewLoadWASMError(err)
	}

	if err := verifyHash(wasmBytes, cfg.Hash); err != nil {
		return NewHashVerificationError(err)
	}

	module, err := m.runtime.LoadComponent(ctx, wasmBytes)
	if err != nil {
		return NewLoadComponentError(err)
	}

	// Pre-compile at registration time to fail fast on missing imports
	if err := module.Compile(ctx); err != nil {
		return NewCompileError(err)
	}

	if err := m.replacePool(entry.ID, cfg, module); err != nil {
		return NewReplacePoolError(err)
	}

	m.configs.Store(entry.ID, cfg)
	m.registerCaller(ctx, entry.ID, cfg.Method)

	m.log.Info("WASM component updated", zap.String("id", entry.ID.String()))
	return nil
}

// Delete removes a WASM component.
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindComponentFunction {
		return NewInvalidEntryKindError(string(entry.Kind), string(api.KindComponentFunction))
	}

	m.removePool(entry.ID)
	m.configs.Delete(entry.ID)
	m.unregisterCaller(ctx, entry.ID)

	m.log.Info("WASM component deleted", zap.String("id", entry.ID.String()))
	return nil
}

// Execute runs a WASM component function by ID.
func (m *Manager) Execute(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	m.mu.RLock()
	entry, exists := m.pools[task.ID]
	m.mu.RUnlock()

	if !exists {
		return nil, NewComponentNotFoundError(task.ID)
	}

	return entry.pool.Call(ctx, entry.config.Method, task.Payloads)
}

// loadWASM reads WASM bytes from the specified filesystem and path.
func (m *Manager) loadWASM(fsID, path string) ([]byte, error) {
	fs, ok := m.fsRegistry.GetFS(fsID)
	if !ok {
		return nil, NewFilesystemNotFoundError(fsID)
	}

	file, err := fs.Open(path)
	if err != nil {
		return nil, NewOpenFileError(path, err)
	}
	defer file.Close()

	return io.ReadAll(file)
}

// verifyHash checks that the WASM bytes match the expected hash.
func verifyHash(data []byte, expected string) error {
	// Expected format: "sha256:hexstring"
	parts := strings.SplitN(expected, ":", 2)
	if len(parts) != 2 {
		return NewInvalidHashFormatError(expected)
	}

	algorithm := parts[0]
	expectedHash := parts[1]

	var actualHash string
	switch algorithm {
	case "sha256":
		h := sha256.Sum256(data)
		actualHash = hex.EncodeToString(h[:])
	default:
		return NewUnsupportedHashAlgorithmError(algorithm)
	}

	if actualHash != expectedHash {
		return NewHashMismatchError(expectedHash, actualHash)
	}

	return nil
}

// createPool creates a new pool for a WASM component.
func (m *Manager) createPool(id registry.ID, cfg *api.ComponentFunctionConfig, module *wasmrt.Module) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return ErrManagerNotStarted
	}

	// Look up transport if specified
	var transport api.Transport
	if cfg.Transport != "" && m.transports != nil {
		transport = m.transports.Get(cfg.Transport)
	}

	factory := engine.NewFactoryWithTransport(m.runtime, module, transport)
	poolCfg := cfg.Pool.ToPoolConfig()
	poolType := cfg.Pool.Type
	if poolType == "" {
		poolType = api.PoolTypeStatic
	}

	var pool funcpool.Pool
	var err error

	switch poolType {
	case api.PoolTypeInline, api.PoolTypeLazy:
		pool, err = funcpool.NewInline(factory.Create(), m.dispatcher)

	case api.PoolTypeStatic:
		pool, err = funcpool.NewStatic(factory.Create(), m.dispatcher, funcpool.Config{
			Workers:   poolCfg.Workers,
			QueueSize: poolCfg.QueueSize,
		})

	default:
		return NewUnknownPoolTypeError(poolType)
	}

	if err != nil {
		return NewCreatePoolError(err)
	}

	m.pools[id] = &poolEntry{
		pool:   pool,
		config: *cfg,
		module: module,
	}

	pool.Start()
	return nil
}

// replacePool replaces an existing pool.
func (m *Manager) replacePool(id registry.ID, cfg *api.ComponentFunctionConfig, module *wasmrt.Module) error {
	m.mu.Lock()
	if entry, exists := m.pools[id]; exists {
		entry.pool.Stop()
		delete(m.pools, id)
	}
	m.mu.Unlock()

	return m.createPool(id, cfg, module)
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

// unpackConfig extracts ComponentFunctionConfig from a registry entry.
func unpackConfig(ctx context.Context, entry registry.Entry) (*api.ComponentFunctionConfig, error) {
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		return nil, NewTranscoderNotFoundError()
	}

	cfg := &api.ComponentFunctionConfig{}
	if err := dtt.Unmarshal(entry.Data, cfg); err != nil {
		return nil, NewUnmarshalConfigError(err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Compile-time check
var _ registry.EntryListener = (*Manager)(nil)
