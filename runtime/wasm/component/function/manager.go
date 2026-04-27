// SPDX-License-Identifier: MPL-2.0

// Package function provides WASM function management.
package function

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/api/topology"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmengine "github.com/wippyai/runtime/runtime/wasm/engine"
	funcpool "github.com/wippyai/runtime/system/scheduler/pool"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

type configEntry struct {
	options   runtime.Options
	wat       *api.WATFunctionConfig
	wasm      *api.FunctionConfig
	method    string
	transport string
	kind      registry.Kind
	wasi      api.WASIConfig
	pool      api.PoolConfig
	limits    api.LimitsConfig
}

// poolEntry is one callable generation. Relay host registration is non-replacing,
// so every generation gets a unique host and retired generations stay alive until
// active calls release them.
type poolEntry struct {
	drained  chan struct{}
	pool     funcpool.Pool
	method   string
	hostID   string
	mu       sync.Mutex
	active   int
	stopOnce sync.Once
	retired  bool
}

// Manager handles WASM function loading, pooling and execution.
type Manager struct {
	node         relay.Node
	topo         topology.Topology
	bus          event.Bus
	dispatcher   dispatcher.Dispatcher
	fsRegistry   fsapi.Registry
	pidReg       topology.PIDRegistry
	ctx          context.Context
	configs      map[registry.ID]*configEntry
	log          *zap.Logger
	pools        map[registry.ID]*poolEntry
	coreRT       *wasmrt.Runtime
	componentRT  *wasmrt.Runtime
	hostRegistry *wasmcomponent.HostRegistry
	mu           sync.RWMutex
	hostSeq      atomic.Uint64
	started      bool
}

// NewManager creates a new WASM function manager.
func NewManager(
	log *zap.Logger,
	bus event.Bus,
	disp dispatcher.Dispatcher,
	fsRegistry fsapi.Registry,
) *Manager {
	return &Manager{
		log:          log,
		bus:          bus,
		dispatcher:   disp,
		fsRegistry:   fsRegistry,
		pools:        make(map[registry.ID]*poolEntry),
		configs:      make(map[registry.ID]*configEntry),
		hostRegistry: wasmcomponent.NewHostRegistry(),
	}
}

// Start initializes runtime dependencies.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.RLock()
	if m.started {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()

	m.ctx = ctx
	m.topo = topology.GetTopology(ctx)
	m.pidReg = topology.GetRegistry(ctx)
	m.node = relay.GetNode(ctx)

	coreRT, err := wasmrt.New(ctx)
	if err != nil {
		return err
	}

	componentRT, err := wasmrt.New(ctx)
	if err != nil {
		_ = coreRT.Close(ctx)
		return err
	}

	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		_ = componentRT.Close(ctx)
		_ = coreRT.Close(ctx)
		return nil
	}
	m.coreRT = coreRT
	m.componentRT = componentRT
	m.started = true
	entries := make([]*poolEntry, 0, len(m.pools))
	for _, entry := range m.pools {
		entries = append(entries, entry)
	}
	m.mu.Unlock()
	m.hostRegistry.ResetLoaded()

	for _, entry := range entries {
		entry.pool.Start()
		if m.node != nil && entry.hostID != "" {
			if err := m.node.RegisterHost(entry.hostID, entry.pool); err != nil {
				entry.pool.Stop()
				return err
			}
		}
	}

	m.log.Info("wasm function manager started")
	return nil
}

// Stop stops pools and closes runtime.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, entry := range m.pools {
		entry.pool.Stop()
		if m.node != nil {
			m.node.UnregisterHost(entry.hostID)
		}
		m.log.Debug("pool stopped", zap.String("id", id.String()))
	}
	m.pools = make(map[registry.ID]*poolEntry)
	m.started = false

	if m.componentRT != nil {
		_ = m.componentRT.Close(context.Background())
		m.componentRT = nil
	}
	if m.coreRT != nil {
		_ = m.coreRT.Close(context.Background())
		m.coreRT = nil
	}
	m.hostRegistry.ResetLoaded()

	m.log.Info("wasm function manager stopped")
}

func (m *Manager) storeConfig(id registry.ID, cfg *configEntry) {
	m.mu.Lock()
	m.configs[id] = cfg
	m.mu.Unlock()
}

func (m *Manager) getConfig(id registry.ID) *configEntry {
	m.mu.RLock()
	cfg := m.configs[id]
	m.mu.RUnlock()
	return cfg
}

func (m *Manager) deleteConfig(id registry.ID) {
	m.mu.Lock()
	delete(m.configs, id)
	m.mu.Unlock()
}

func (m *Manager) runtimeInstance(component bool) *wasmrt.Runtime {
	m.mu.RLock()
	var rt *wasmrt.Runtime
	if component {
		rt = m.componentRT
	} else {
		rt = m.coreRT
	}
	m.mu.RUnlock()
	return rt
}

func (m *Manager) processFactory(cfg *configEntry, module *wasmrt.Module) *wasmengine.Factory {
	return wasmengine.NewFactory(module, cfg.transport, cfg.wasi, cfg.limits, m.fsRegistry)
}

var _ registry.EntryListener = (*Manager)(nil)
