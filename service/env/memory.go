package env

import (
	"context"
	"sync"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.uber.org/zap"
)

// MemoryStorage implements env.Storage interface using in-memory map
type MemoryStorage struct {
	values sync.Map
	log    *zap.Logger
}

// NewMemoryStorage creates a new memory-based storage
func NewMemoryStorage(defaultValues map[string]string, log *zap.Logger) *MemoryStorage {
	storage := &MemoryStorage{
		values: sync.Map{},
		log:    log.With(zap.String("component", "memstorage")),
	}

	for key, value := range defaultValues {
		storage.values.Store(key, value)
	}

	return storage
}

// Get retrieves a value from storage
func (s *MemoryStorage) Get(ctx context.Context, key string) (string, error) {
	value, exists := s.values.Load(key)
	if !exists {
		return "", nil
	}

	strValue, ok := value.(string)
	if !ok {
		return "", nil
	}
	return strValue, nil
}

// Set stores a value in storage
func (s *MemoryStorage) Set(ctx context.Context, key, value string) error {
	s.values.Store(key, value)
	return nil
}

// Delete removes a value from storage
func (s *MemoryStorage) Delete(ctx context.Context, key string) error {
	s.values.Delete(key)
	return nil
}

// List returns all variable names and values in this storage
func (s *MemoryStorage) List(ctx context.Context) (map[string]string, error) {
	result := make(map[string]string)
	s.values.Range(func(key, value interface{}) bool {
		if strKey, ok := key.(string); ok {
			if strValue, ok := value.(string); ok {
				result[strKey] = strValue
			}
		}
		return true
	})
	return result, nil
}

// Start implements supervisor.Service interface
func (s *MemoryStorage) Start(ctx context.Context) (<-chan any, error) {
	// Memory storage is always ready, no need for actual startup
	statusCh := make(chan any, 1)
	statusCh <- supervisor.Running
	return statusCh, nil
}

// Stop implements supervisor.Service interface
func (s *MemoryStorage) Stop(ctx context.Context) error {
	// Memory storage doesn't need cleanup, just return nil
	return nil
}

// Acquire implements resource.Provider interface
func (s *MemoryStorage) Acquire(ctx context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, resource.ErrResourceLocked
	}

	return &memoryResource{
		storage: s,
		id:      id,
		closed:  false,
		mu:      sync.Mutex{},
	}, nil
}

// memoryResource represents an acquired memory storage resource
type memoryResource struct {
	storage *MemoryStorage
	id      registry.ID
	closed  bool
	mu      sync.Mutex
}

// Get implements resource.Resource interface
func (r *memoryResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, resource.ErrResourceClosed
	}

	return r.storage, nil
}

// Release implements resource.Resource interface
func (r *memoryResource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}

	r.closed = true
}
