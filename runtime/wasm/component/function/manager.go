// Package function provides WASM function management.
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
	api "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/api/topology"
	wasmengine "github.com/wippyai/runtime/runtime/wasm/engine"
	funcpool "github.com/wippyai/runtime/system/scheduler/pool"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
	"go.uber.org/zap"
)

type configEntry struct {
	options   runtime.Options
	wat       *api.WATFunctionConfig
	wasm      *api.WASMFunctionConfig
	method    string
	transport string
	pool      api.PoolConfig
	limits    api.LimitsConfig
	kind      registry.Kind
}

type poolEntry struct {
	pool   funcpool.Pool
	method string
}

// Manager handles WASM function loading, pooling and execution.
type Manager struct {
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
	runtime    *wasmrt.Runtime
	wasi       *preview2.WASI
	mu         sync.RWMutex
	started    bool
}

// NewManager creates a new WASM function manager.
func NewManager(
	log *zap.Logger,
	bus event.Bus,
	disp dispatcher.Dispatcher,
	fsRegistry fsapi.Registry,
) *Manager {
	return &Manager{
		log:        log,
		bus:        bus,
		dispatcher: disp,
		fsRegistry: fsRegistry,
		pools:      make(map[registry.ID]*poolEntry),
		configs:    make(map[registry.ID]*configEntry),
	}
}

// Start initializes runtime dependencies.
func (m *Manager) Start(ctx context.Context) error {
	m.ctx = ctx
	m.topo = topology.GetTopology(ctx)
	m.pidReg = topology.GetRegistry(ctx)
	m.node = relay.GetNode(ctx)

	rt, err := wasmrt.New(ctx)
	if err != nil {
		return err
	}

	wasi := preview2.New()
	if err := rt.RegisterWASI(wasi); err != nil {
		_ = rt.Close(ctx)
		return err
	}

	m.mu.Lock()
	m.runtime = rt
	m.wasi = wasi
	m.started = true
	m.mu.Unlock()

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
			m.node.UnregisterHost(id.String())
		}
		m.log.Debug("pool stopped", zap.String("id", id.String()))
	}
	m.pools = make(map[registry.ID]*poolEntry)
	m.started = false

	if m.wasi != nil {
		m.wasi.Close()
		m.wasi = nil
	}
	if m.runtime != nil {
		_ = m.runtime.Close(context.Background())
		m.runtime = nil
	}

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

func (m *Manager) runtimeInstance() *wasmrt.Runtime {
	m.mu.RLock()
	rt := m.runtime
	m.mu.RUnlock()
	return rt
}

func (m *Manager) processFactory(cfg *configEntry, module *wasmrt.Module) *wasmengine.Factory {
	return wasmengine.NewFactory(module, cfg.transport, cfg.limits)
}

var _ registry.EntryListener = (*Manager)(nil)
