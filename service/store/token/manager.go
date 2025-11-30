package token

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/api/service/tokenstore"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"go.uber.org/zap"
)

// Manager manages token store lifecycle and serves as resource provider
type Manager struct {
	log         *zap.Logger
	dtt         payload.Transcoder
	bus         event.Bus
	resources   resource.Registry
	secRegistry security.Registry
	mu          sync.RWMutex
	configs     map[registry.ID]*tokenstore.Config
	stores      map[registry.ID]*TokenStore // Cache of created token stores
}

// NewManager creates a new token store manager
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	resources resource.Registry,
	secRegistry security.Registry,
	log *zap.Logger,
) *Manager {
	return &Manager{
		log:         log,
		dtt:         dtt,
		bus:         bus,
		resources:   resources,
		secRegistry: secRegistry,
		configs:     make(map[registry.ID]*tokenstore.Config),
		stores:      make(map[registry.ID]*TokenStore),
	}
}

// Add implements registry.EntryListener - registers token store configuration
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != tokenstore.KindTokenStore {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[entry.ID]; exists {
		return fmt.Errorf("token store %s already exists", entry.ID)
	}

	// Decode and initialize configuration
	cfg, err := entryutil.DecodeEntryConfig[tokenstore.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	cfg.Store = cfg.Store.WithDefaultNS(entry.ID.NS)

	// Store the configuration (actual token store will be created during acquisition)
	m.configs[entry.ID] = cfg

	// Register as resource provider (this manager is the provider)
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID:       entry.ID,
			Provider: m,
			Meta:     entry.Meta,
		},
	})

	m.log.Info("registered token store configuration",
		zap.String("id", entry.ID.String()),
		zap.String("store", cfg.Store.String()))

	return nil
}

// Update implements registry.EntryListener - updates token store configuration
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != tokenstore.KindTokenStore {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[entry.ID]; !exists {
		return fmt.Errorf("token store %s not found", entry.ID)
	}

	// Decode and initialize updated configuration
	cfg, err := entryutil.DecodeEntryConfig[tokenstore.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	cfg.Store = cfg.Store.WithDefaultNS(entry.ID.NS)

	// Update configuration
	m.configs[entry.ID] = cfg

	// If we've already created a store for this ID, remove it so it gets recreated
	// with the new configuration on next acquisition
	delete(m.stores, entry.ID)

	// Update resource registration
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Update,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID:       entry.ID,
			Provider: m,
			Meta:     entry.Meta,
		},
	})

	m.log.Info("updated token store configuration",
		zap.String("id", entry.ID.String()),
		zap.String("store", cfg.Store.String()))

	return nil
}

// Delete implements registry.EntryListener - removes token store configuration
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != tokenstore.KindTokenStore {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[entry.ID]; !exists {
		return fmt.Errorf("token store %s not found", entry.ID)
	}

	// Remove configuration and any cached store
	delete(m.configs, entry.ID)
	delete(m.stores, entry.ID)

	// Unregister resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   entry.ID.String(),
		Data:   entry.ID,
	})

	m.log.Info("deleted token store configuration",
		zap.String("id", entry.ID.String()))

	return nil
}

// Acquire implements resource.Provider - creates and returns a token store
func (m *Manager) Acquire(_ context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	m.mu.RLock()
	cfg, exists := m.configs[id]

	// Check if we already have a cached store
	store, storeExists := m.stores[id]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("token store %s not found", id)
	}

	// Only support normal mode
	if mode != resource.ModeNormal {
		return nil, resource.ErrResourceLocked
	}

	// Create the store if it doesn't exist yet
	if !storeExists {
		m.mu.Lock()
		// Check again in case another goroutine created it while we were waiting
		store, storeExists = m.stores[id]
		if !storeExists {
			var err error
			store, err = NewStoreTokenStore(cfg, m.dtt, m.resources, m.secRegistry)
			if err != nil {
				m.mu.Unlock()
				return nil, fmt.Errorf("failed to create token store: %w", err)
			}
			m.stores[id] = store
		}
		m.mu.Unlock()
	}

	// Create and return token store resource
	return &tokenStoreResource{
		store: store,
	}, nil
}

// tokenStoreResource is a resource that provides access to a token store
type tokenStoreResource struct {
	store  *TokenStore
	mu     sync.Mutex
	closed bool
}

// Get implements resource.Resource
func (r *tokenStoreResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, resource.ErrResourceReleased
	}

	return r.store, nil
}

// Release implements resource.Resource
func (r *tokenStoreResource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}

	r.closed = true
}
