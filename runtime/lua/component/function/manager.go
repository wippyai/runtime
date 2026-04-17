// SPDX-License-Identifier: MPL-2.0

// Package function provides Lua function management.
// Uses pluggable pool schedulers for different workload patterns.
package function

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	funcpool "github.com/wippyai/runtime/system/scheduler/pool"
	"go.uber.org/zap"
)

// configEntry holds config for either source or bytecode function.
type configEntry struct {
	options  runtime.Options
	source   *api.FunctionConfig
	bytecode *api.BytecodeFunctionConfig
	method   string
	pool     api.PoolConfig
}

// poolEntry wraps a pool with its config.
type poolEntry struct {
	pool   funcpool.Pool
	method string
}

// Manager handles both source and bytecode Lua function compilation, pooling and execution.
type Manager struct {
	factory    engine.CompiledFactory
	node       relay.Node
	topo       topology.Topology
	bus        event.Bus
	dispatcher dispatcher.Dispatcher
	fsRegistry fsapi.Registry
	pidReg     topology.PIDRegistry
	ctx        context.Context
	configs    map[registry.ID]*configEntry
	log        *zap.Logger
	pools      map[registry.ID]*poolEntry
	code       *code.Manager
	mu         sync.RWMutex
	started    bool
}

// NewManager creates a new function manager.
// fsRegistry can be nil if bytecode functions are not used.
func NewManager(
	log *zap.Logger,
	code *code.Manager,
	bus event.Bus,
	disp dispatcher.Dispatcher,
	fsRegistry fsapi.Registry,
	factory engine.CompiledFactory,
) *Manager {
	return &Manager{
		log:        log,
		code:       code,
		bus:        bus,
		dispatcher: disp,
		fsRegistry: fsRegistry,
		factory:    factory,
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

	m.log.Info("function manager started")
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

	m.log.Info("function manager stopped")
}

// storeConfig stores a config entry.
func (m *Manager) storeConfig(id registry.ID, cfg *configEntry) {
	m.mu.Lock()
	m.configs[id] = cfg
	m.mu.Unlock()
}

// getConfig retrieves a config entry.
func (m *Manager) getConfig(id registry.ID) *configEntry {
	m.mu.RLock()
	cfg := m.configs[id]
	m.mu.RUnlock()
	return cfg
}

// deleteConfig removes a config entry.
func (m *Manager) deleteConfig(id registry.ID) {
	m.mu.Lock()
	delete(m.configs, id)
	m.mu.Unlock()
}

// Compile-time check
var _ registry.EntryListener = (*Manager)(nil)
