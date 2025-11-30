package memory

import (
	"context"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/service/memstore"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/store"
	"github.com/wippyai/runtime/api/supervisor"
	"go.uber.org/zap"
)

// MemoryStore is an in-memory implementation of the store.Store interface
// that also functions as a resource.Provider and a supervisor.Service
type MemoryStore struct {
	id         registry.ID
	config     *memstore.MemoryConfig
	log        *zap.Logger
	mu         sync.RWMutex
	data       map[string]*storeEntry
	closed     bool
	statusChan chan any
	stopChan   chan struct{}
	wg         sync.WaitGroup // For tracking active goroutines
}

// storeEntry represents a single key-value pair with metadata
type storeEntry struct {
	value      payload.Payload
	expiration *time.Time
	lastAccess time.Time
}

// NewMemoryStore creates a new in-memory key-value store
func NewMemoryStore(id registry.ID, config *memstore.MemoryConfig, log *zap.Logger) *MemoryStore {
	if config == nil {
		config = &memstore.MemoryConfig{}
	}

	return &MemoryStore{
		id:         id,
		config:     config,
		log:        log.With(zap.String("component", "memstore"), zap.String("id", id.String())),
		data:       make(map[string]*storeEntry),
		statusChan: make(chan any, 10), // Buffer for status messages
		stopChan:   make(chan struct{}),
	}
}

// Start implements supervisor.Service interface
// Starts the cleanup goroutine if cleanup interval is configured
func (m *MemoryStore) Start(ctx context.Context) (<-chan any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, store.ErrStoreClosed
	}

	// Serve cleanup goroutine if cleanup interval is set
	if m.config.CleanupInterval > 0 {
		m.wg.Add(1)
		go m.cleanupLoop(ctx)
		m.log.Info("started cleanup routine",
			zap.Duration("interval", m.config.CleanupInterval),
			zap.Int("max_size", m.config.MaxSize))
	}

	select {
	case m.statusChan <- "memory store started":
	default:
	}

	return m.statusChan, nil
}

// Stop implements supervisor.Service interface
// Stops the cleanup goroutine and waits for it to finish
func (m *MemoryStore) Stop(ctx context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}

	m.closed = true
	close(m.stopChan)
	m.mu.Unlock()

	// Wait for cleanup goroutine to finish with timeout
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.log.Info("memory store stopped cleanly")
		return nil
	case <-ctx.Done():
		m.log.Warn("memory store stop timed out")
		return ctx.Err()
	}
}

// Get retrieves a value by key
func (m *MemoryStore) Get(_ context.Context, key registry.ID) (payload.Payload, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, store.ErrStoreClosed
	}

	keyStr := key.String()
	entry, exists := m.data[keyStr]
	if !exists {
		return nil, store.ErrKeyNotFound
	}

	// Check if entry has expired
	if entry.expiration != nil && time.Now().After(*entry.expiration) {
		// Remove expired entry (need to unlock and relock)
		m.mu.RUnlock()
		m.mu.Lock()
		delete(m.data, keyStr)
		m.mu.Unlock()
		m.mu.RLock()

		return nil, store.ErrKeyNotFound
	}

	// Update last access time (requires write lock)
	m.mu.RUnlock()
	m.mu.Lock()
	if entry, exists = m.data[keyStr]; exists {
		entry.lastAccess = time.Now()
	}
	m.mu.Unlock()
	m.mu.RLock()

	return entry.value, nil
}

// Set stores or updates a value with the given key
func (m *MemoryStore) Set(_ context.Context, entry store.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return store.ErrStoreClosed
	}

	keyStr := entry.Key.String()

	// Check if we're at capacity and need to reject
	if m.config.MaxSize > 0 && len(m.data) >= m.config.MaxSize && m.data[keyStr] == nil {
		return store.ErrStoreFull
	}

	// Calculate expiration time if TTL is set
	var expiration *time.Time
	if entry.TTL > 0 {
		exp := time.Now().Add(entry.TTL)
		expiration = &exp
	}

	// Store the entry
	m.data[keyStr] = &storeEntry{
		value:      entry.Value,
		expiration: expiration,
		lastAccess: time.Now(),
	}

	return nil
}

// Delete removes a value with the given key
func (m *MemoryStore) Delete(_ context.Context, key registry.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return store.ErrStoreClosed
	}

	keyStr := key.String()
	if _, exists := m.data[keyStr]; !exists {
		return store.ErrKeyNotFound
	}

	delete(m.data, keyStr)
	return nil
}

// Has checks if a key exists without retrieving the value
func (m *MemoryStore) Has(_ context.Context, key registry.ID) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return false, store.ErrStoreClosed
	}

	keyStr := key.String()
	entry, exists := m.data[keyStr]
	if !exists {
		return false, nil
	}

	// Check if entry has expired
	if entry.expiration != nil && time.Now().After(*entry.expiration) {
		// Remove expired entry (need to unlock and relock)
		m.mu.RUnlock()
		m.mu.Lock()
		delete(m.data, keyStr)
		m.mu.Unlock()
		m.mu.RLock()

		return false, nil
	}

	return true, nil
}

// cleanupLoop periodically checks for and removes expired entries
func (m *MemoryStore) cleanupLoop(ctx context.Context) {
	defer m.wg.Done()
	ticker := time.NewTicker(m.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.cleanup()
		case <-m.stopChan:
			m.log.Debug("cleanup routine stopped")
			return
		case <-ctx.Done():
			m.log.Debug("cleanup routine stopped by context")
			return
		}
	}
}

// cleanup removes expired entries
func (m *MemoryStore) cleanup() {
	now := time.Now()
	expired := 0

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return
	}

	for key, entry := range m.data {
		if entry.expiration != nil && now.After(*entry.expiration) {
			delete(m.data, key)
			expired++
		}
	}

	if expired > 0 {
		m.log.Debug("removed expired entries", zap.Int("count", expired))
	}
}

// Acquire implements resource.Provider interface
func (m *MemoryStore) Acquire(_ context.Context, _ registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, resource.ErrResourceReleased
	}

	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, resource.ErrResourceLocked
	}

	return &storeResource{store: m}, nil
}

// storeResource represents an acquired store resource
type storeResource struct {
	store  *MemoryStore
	closed bool
	mu     sync.Mutex
}

// Get implements resource.Resource interface
func (r *storeResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, resource.ErrResourceReleased
	}

	return store.Store(r.store), nil
}

// Release implements resource.Resource interface
func (r *storeResource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}

	r.closed = true
}

// Ensure MemoryStore implements all required interfaces
var (
	_ store.Store        = (*MemoryStore)(nil)
	_ resource.Provider  = (*MemoryStore)(nil)
	_ supervisor.Service = (*MemoryStore)(nil)
)
