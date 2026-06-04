// SPDX-License-Identifier: MPL-2.0

package memory

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	memstore "github.com/wippyai/runtime/api/service/store/memory"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/store"
	"github.com/wippyai/runtime/api/supervisor"
	servicestore "github.com/wippyai/runtime/service/store"
	systemresource "github.com/wippyai/runtime/system/resource"
	"go.uber.org/zap"
)

var (
	_ store.Store        = (*Store)(nil)
	_ store.InfoProvider = (*Store)(nil)
	_ store.EntryReader  = (*Store)(nil)
	_ store.Lister       = (*Store)(nil)
	_ store.Putter       = (*Store)(nil)
	_ resource.Provider  = (*Store)(nil)
	_ supervisor.Service = (*Store)(nil)
)

// Store is an in-memory implementation of the store.Store interface
// that also functions as a resource.Provider and a supervisor.Service
type Store struct {
	config     *memstore.Config
	log        *zap.Logger
	data       map[string]*storeEntry
	statusChan chan any
	stopChan   chan struct{}
	id         registry.ID
	wg         sync.WaitGroup
	mu         sync.RWMutex
	closed     bool
	version    store.Version
}

// storeEntry represents a single key-value pair with metadata
type storeEntry struct {
	value      payload.Payload
	expiration *time.Time
	lastAccess time.Time
	version    store.Version
}

// NewStore creates a new in-memory key-value store
func NewStore(id registry.ID, config *memstore.Config, log *zap.Logger) *Store {
	if config == nil {
		config = &memstore.Config{}
	}
	if log == nil {
		log = zap.NewNop()
	}

	return &Store{
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
func (m *Store) Start(ctx context.Context) (<-chan any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, servicestore.ErrStoreClosed
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
func (m *Store) Stop(ctx context.Context) error {
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
func (m *Store) Get(_ context.Context, key registry.ID) (payload.Payload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, servicestore.ErrStoreClosed
	}

	keyStr := key.String()
	entry, exists := m.data[keyStr]
	if !exists {
		return nil, store.ErrKeyNotFound
	}

	// Check if entry has expired
	if entry.expiration != nil && time.Now().After(*entry.expiration) {
		delete(m.data, keyStr)
		return nil, store.ErrKeyNotFound
	}

	entry.lastAccess = time.Now()
	return entry.value, nil
}

// Entry retrieves a value and its monotonic store version.
func (m *Store) Entry(_ context.Context, key registry.ID) (store.VersionedEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return store.VersionedEntry{}, servicestore.ErrStoreClosed
	}

	keyStr := key.String()
	entry, exists := m.data[keyStr]
	if !exists {
		return store.VersionedEntry{}, store.ErrKeyNotFound
	}
	if entryExpired(entry, time.Now()) {
		delete(m.data, keyStr)
		return store.VersionedEntry{}, store.ErrKeyNotFound
	}

	entry.lastAccess = time.Now()
	return store.VersionedEntry{
		Entry:   store.Entry{Key: key, Value: entry.value},
		Version: entry.version,
	}, nil
}

// Set stores or updates a value with the given key
func (m *Store) Set(_ context.Context, entry store.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return servicestore.ErrStoreClosed
	}

	now := time.Now()
	keyStr := entry.Key.String()
	m.purgeExpiredLocked(now)
	_, exists := m.data[keyStr]

	// Check if we're at capacity and need to reject
	if m.config.MaxSize > 0 && len(m.data) >= m.config.MaxSize && !exists {
		return servicestore.ErrStoreFull
	}

	// Calculate expiration time if TTL is set
	var expiration *time.Time
	if entry.TTL > 0 {
		exp := now.Add(entry.TTL)
		expiration = &exp
	}

	// Store the entry
	m.version++
	m.data[keyStr] = &storeEntry{
		value:      entry.Value,
		expiration: expiration,
		lastAccess: now,
		version:    m.version,
	}

	return nil
}

// Put stores a value with optional absent/version preconditions.
func (m *Store) Put(_ context.Context, key registry.ID, value payload.Payload, opts store.PutOptions) (store.VersionedEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return store.VersionedEntry{}, servicestore.ErrStoreClosed
	}
	if opts.OnlyIfAbsent && opts.HasVersion {
		return store.VersionedEntry{}, store.ErrInvalidOptions
	}
	if opts.HasVersion && opts.Version == 0 {
		return store.VersionedEntry{}, store.ErrInvalidOptions
	}
	if opts.TTL < 0 {
		return store.VersionedEntry{}, store.ErrInvalidOptions
	}

	now := time.Now()
	keyStr := key.String()
	m.purgeExpiredLocked(now)
	existing, exists := m.data[keyStr]
	if opts.OnlyIfAbsent && exists {
		return store.VersionedEntry{}, store.ErrKeyExists
	}
	if opts.HasVersion {
		if !exists {
			return store.VersionedEntry{}, store.ErrKeyNotFound
		}
		if existing.version != opts.Version {
			return store.VersionedEntry{}, store.ErrVersionMismatch
		}
	}

	if m.config.MaxSize > 0 && len(m.data) >= m.config.MaxSize && !exists {
		return store.VersionedEntry{}, servicestore.ErrStoreFull
	}

	var expiration *time.Time
	if opts.TTL > 0 {
		exp := now.Add(opts.TTL)
		expiration = &exp
	}

	m.version++
	version := m.version
	m.data[keyStr] = &storeEntry{
		value:      value,
		expiration: expiration,
		lastAccess: now,
		version:    version,
	}
	return store.VersionedEntry{
		Entry:   store.Entry{Key: key, Value: value, TTL: opts.TTL},
		Version: version,
	}, nil
}

// Delete removes a value with the given key
func (m *Store) Delete(_ context.Context, key registry.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return servicestore.ErrStoreClosed
	}

	keyStr := key.String()
	entry, exists := m.data[keyStr]
	if !exists {
		return store.ErrKeyNotFound
	}
	if entryExpired(entry, time.Now()) {
		delete(m.data, keyStr)
		return store.ErrKeyNotFound
	}

	delete(m.data, keyStr)
	return nil
}

// Has checks if a key exists without retrieving the value
func (m *Store) Has(_ context.Context, key registry.ID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return false, servicestore.ErrStoreClosed
	}

	keyStr := key.String()
	entry, exists := m.data[keyStr]
	if !exists {
		return false, nil
	}

	// Check if entry has expired
	if entry.expiration != nil && time.Now().After(*entry.expiration) {
		delete(m.data, keyStr)
		return false, nil
	}

	return true, nil
}

// List returns a deterministic page of non-expired entries.
func (m *Store) List(_ context.Context, opts store.ListOptions) (store.Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return store.Page{}, servicestore.ErrStoreClosed
	}

	now := time.Now()
	items := make([]store.VersionedEntry, 0, len(m.data))
	for keyStr, entry := range m.data {
		if entryExpired(entry, now) {
			delete(m.data, keyStr)
			continue
		}
		if opts.Prefix != "" && !strings.HasPrefix(keyStr, opts.Prefix) {
			continue
		}
		items = append(items, store.VersionedEntry{
			Entry:   store.Entry{Key: registry.ParseID(keyStr), Value: entry.value},
			Version: entry.version,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Key.String() < items[j].Key.String()
	})
	return store.PageFromSorted(items, opts), nil
}

// StoreInfo reports the memory store's stable capabilities.
func (m *Store) StoreInfo(_ context.Context) store.Info {
	return store.Info{
		ID:             m.id,
		Backend:        store.BackendMemory,
		Consistency:    store.ConsistencyLocal,
		Durable:        false,
		List:           true,
		Versioned:      true,
		ConditionalPut: true,
		TTL:            true,
	}
}

// cleanupLoop periodically checks for and removes expired entries
func (m *Store) cleanupLoop(ctx context.Context) {
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
func (m *Store) cleanup() {
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return
	}

	expired := m.purgeExpiredLocked(now)
	if expired > 0 {
		m.log.Debug("removed expired entries", zap.Int("count", expired))
	}
}

func (m *Store) purgeExpiredLocked(now time.Time) int {
	expired := 0
	for key, entry := range m.data {
		if entryExpired(entry, now) {
			delete(m.data, key)
			expired++
		}
	}
	return expired
}

func entryExpired(entry *storeEntry, now time.Time) bool {
	return entry.expiration != nil && now.After(*entry.expiration)
}

// Acquire implements resource.Provider interface
func (m *Store) Acquire(_ context.Context, _ registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, resource.ErrReleased
	}

	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, systemresource.ErrLocked
	}

	return &storeResource{store: m}, nil
}

// storeResource represents an acquired store resource
type storeResource struct {
	store  *Store
	closed bool
	mu     sync.Mutex
}

// Get implements resource.Resource interface
func (r *storeResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, resource.ErrReleased
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

// Ensure Store implements all required interfaces
var (
	_ store.Store        = (*Store)(nil)
	_ store.InfoProvider = (*Store)(nil)
	_ store.EntryReader  = (*Store)(nil)
	_ store.Lister       = (*Store)(nil)
	_ store.Putter       = (*Store)(nil)
	_ resource.Provider  = (*Store)(nil)
	_ supervisor.Service = (*Store)(nil)
)
