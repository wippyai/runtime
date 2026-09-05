// SPDX-License-Identifier: MPL-2.0

// Package process provides WASM process management.
package process

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/api/security"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	"go.uber.org/zap"
)

type configEntry struct {
	cfg         *api.ProcessConfig
	factory     *ActorFactory
	security    *security.Config
	bytes       []byte
	isComponent bool
}

// Manager handles WASM process loading and process factory registration.
type Manager struct {
	log          *zap.Logger
	bus          event.Bus
	fsRegistry   fsapi.Registry
	hostRegistry *wasmcomponent.HostRegistry
	configs      map[registry.ID]*configEntry
	mu           sync.RWMutex
	opMu         sync.Mutex
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
	if err := ctx.Err(); err != nil {
		return err
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.mu.Lock()
	m.started = true
	m.mu.Unlock()
	m.hostRegistry.ResetLoaded()

	m.log.Info("wasm process manager started")
	return nil
}

// Stop invalidates all process factories, prevents new factory registrations,
// releases manager-level shared host resources, and resets loaded host state.
//
// Note on actor lifecycle ownership:
// Manager.Stop does NOT stop, cancel, or join active running actors or close
// their individual backend runtimes. Each actor process owns its dedicated runtime
// and resource lifecycle; running actors are owned and supervised by the process host
// scheduler (service/host.Host). Host.Stop cancels and drains those actors.
// Stop here guarantees that all
// factories are closed (so no subsequent spawns can occur) and that no factory registration
// can be published after Stop has executed.
func (m *Manager) Stop() {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.mu.Lock()
	m.started = false
	configs := m.configs
	m.configs = make(map[registry.ID]*configEntry)
	m.mu.Unlock()

	for _, cfg := range configs {
		if cfg != nil && cfg.factory != nil {
			cfg.factory.Close()
		}
	}
	m.hostRegistry.CloseResources()
	m.hostRegistry.ResetLoaded()

	m.log.Info("wasm process manager stopped")
}

func (m *Manager) isStarted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
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

func (m *Manager) deleteConfig(id registry.ID) *configEntry {
	m.mu.Lock()
	cfg := m.configs[id]
	delete(m.configs, id)
	m.mu.Unlock()
	return cfg
}

var _ registry.EntryListener = (*Manager)(nil)
