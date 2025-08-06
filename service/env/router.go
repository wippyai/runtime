package env

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.uber.org/zap"
)

// RouterStorage implements env.Storage interface with fallback mechanism
// Reads are attempted from storages in order until a value is found
// Writes always go to the primary (first) storage
type RouterStorage struct {
	storages []env.Storage
	log      *zap.Logger
	mu       sync.RWMutex
}

// IsRouterStorage checks if a storage is a router storage
func IsRouterStorage(storage env.Storage) bool {
	_, ok := storage.(*RouterStorage)
	return ok
}

// NewRouterStorage creates a new router storage with the specified storages
func NewRouterStorage(storages []env.Storage, log *zap.Logger) (*RouterStorage, error) {
	if len(storages) == 0 {
		return nil, fmt.Errorf("at least one storage must be provided")
	}

	return &RouterStorage{
		storages: storages,
		log:      log.With(zap.String("component", "routerstorage")),
	}, nil
}

// Get retrieves a value from storage with fallback mechanism
func (r *RouterStorage) Get(ctx context.Context, name string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var lastErr error
	for _, storage := range r.storages {
		value, err := storage.Get(ctx, name)
		if err == nil && value != "" {
			return value, nil
		}
		if err != nil {
			lastErr = err
		}
	}

	return "", lastErr
}

// Set stores a value in the primary storage only
func (r *RouterStorage) Set(ctx context.Context, name, value string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	r.log.Debug("setting variable in primary storage",
		zap.String("variable", name),
		zap.String("value", value))

	return r.storages[0].Set(ctx, name, value)
}

// Delete removes a value from the primary storage only
func (r *RouterStorage) Delete(ctx context.Context, name string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	r.log.Debug("deleting variable from primary storage",
		zap.String("variable", name))

	return r.storages[0].Delete(ctx, name)
}

// List returns all variables from all storages (with primary taking precedence for duplicates)
func (r *RouterStorage) List(ctx context.Context) (map[string]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]string)

	// Iterate through all storages in order, with earlier storages taking precedence
	for i, storage := range r.storages {
		storageVars, err := storage.List(ctx)
		if err != nil {
			r.log.Warn("failed to list variables from storage",
				zap.Int("storage_index", i),
				zap.Error(err))
			continue
		}

		// Add variables from this storage (only if not already present from earlier storages)
		for k, v := range storageVars {
			if _, exists := result[k]; !exists {
				result[k] = v
			}
		}
	}

	return result, nil
}

// Start implements supervisor.Service
func (r *RouterStorage) Start(_ context.Context) (<-chan any, error) {
	r.log.Info("starting router storage")
	statusCh := make(chan any, 1)
	statusCh <- supervisor.Running
	return statusCh, nil
}

// Stop implements supervisor.Service
func (r *RouterStorage) Stop(_ context.Context) error {
	r.log.Info("stopping router storage")
	return nil
}

// Acquire implements resource.Provider
func (r *RouterStorage) Acquire(_ context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, resource.ErrResourceLocked
	}

	return &routerResource{
		storage: r,
		id:      id,
		closed:  false,
		mu:      sync.Mutex{},
	}, nil
}

// routerResource implements resource.Resource
type routerResource struct {
	storage *RouterStorage
	id      registry.ID
	closed  bool
	mu      sync.Mutex
}

func (r *routerResource) ID() registry.ID {
	return r.id
}

func (r *routerResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, resource.ErrResourceReleased
	}

	return r.storage, nil
}

func (r *routerResource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}

	r.closed = true
}
