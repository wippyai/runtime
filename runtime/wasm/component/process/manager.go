// Package process provides WASM process management.
package process

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/wasm"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

type configEntry struct {
	wasm      *api.WASMFunctionConfig
	method    string
	transport string
	wasi      api.WASIConfig
	limits    api.LimitsConfig
}

// Manager handles WASM process loading and process factory registration.
type Manager struct {
	log          *zap.Logger
	bus          event.Bus
	fsRegistry   fsapi.Registry
	coreRT       *wasmrt.Runtime
	componentRT  *wasmrt.Runtime
	hostRegistry *wasmcomponent.HostRegistry
	configs      map[registry.ID]*configEntry
	mu           sync.RWMutex
	started      bool
}

// NewManager creates a new WASM process manager.
func NewManager(log *zap.Logger, bus event.Bus, fsRegistry fsapi.Registry) *Manager {
	return &Manager{
		log:          log,
		bus:          bus,
		fsRegistry:   fsRegistry,
		configs:      make(map[registry.ID]*configEntry),
		hostRegistry: wasmcomponent.NewHostRegistry(),
	}
}

// Start initializes runtime dependencies.
func (m *Manager) Start(ctx context.Context) error {
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
	m.coreRT = coreRT
	m.componentRT = componentRT
	m.started = true
	m.mu.Unlock()
	m.hostRegistry.ResetLoaded()

	m.log.Info("wasm process manager started")
	return nil
}

// Stop closes runtimes and clears loaded host state.
func (m *Manager) Stop() {
	m.mu.Lock()
	componentRT := m.componentRT
	coreRT := m.coreRT
	m.componentRT = nil
	m.coreRT = nil
	m.started = false
	m.mu.Unlock()

	if componentRT != nil {
		_ = componentRT.Close(context.Background())
	}
	if coreRT != nil {
		_ = coreRT.Close(context.Background())
	}
	m.hostRegistry.ResetLoaded()

	m.log.Info("wasm process manager stopped")
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

var _ registry.EntryListener = (*Manager)(nil)
