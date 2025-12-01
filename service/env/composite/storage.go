package composite

import (
	"context"
	"errors"
	"sync"

	"github.com/wippyai/runtime/api/env"
	enverr "github.com/wippyai/runtime/service/env"
)

// Storage combines multiple storages with fallback and caching.
// Get operations search storages in order until a value is found.
// Set/Delete operations apply only to the first (primary) storage.
type Storage struct {
	storages []env.Storage
	cache    sync.Map
	mu       sync.RWMutex
}

// Verify Storage implements env.Storage
var _ env.Storage = (*Storage)(nil)

// NewStorage creates a composite storage from multiple underlying storages.
// At least one storage must be provided.
func NewStorage(storages []env.Storage) (*Storage, error) {
	if len(storages) == 0 {
		return nil, enverr.ErrNoStorages
	}

	return &Storage{
		storages: storages,
	}, nil
}

// Get retrieves a value by searching storages in order.
// The first storage that has the key (returns nil error) is used.
// Empty string is a valid value.
func (r *Storage) Get(ctx context.Context, name string) (string, error) {
	if cachedValue, exists := r.cache.Load(name); exists {
		if value, ok := cachedValue.(string); ok {
			return value, nil
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, storage := range r.storages {
		value, err := storage.Get(ctx, name)
		if err == nil {
			r.cache.Store(name, value)
			return value, nil
		}
		// Only continue to next storage if key was not found
		// Other errors (permission, IO) should stop the search
		if !errors.Is(err, env.ErrVariableNotFound) {
			return "", err
		}
	}

	return "", env.ErrVariableNotFound
}

// Set stores a value in the primary (first) storage.
func (r *Storage) Set(ctx context.Context, name, value string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	err := r.storages[0].Set(ctx, name, value)
	if err == nil {
		r.cache.Store(name, value)
	}

	return err
}

// Delete removes a value from the primary (first) storage.
func (r *Storage) Delete(ctx context.Context, name string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	err := r.storages[0].Delete(ctx, name)
	if err == nil {
		r.cache.Delete(name)
	}

	return err
}

// List returns all variables from all storages.
// Variables from earlier storages take precedence over later ones.
func (r *Storage) List(ctx context.Context) (map[string]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]string)

	for _, storage := range r.storages {
		storageVars, err := storage.List(ctx)
		if err != nil {
			continue
		}

		for k, v := range storageVars {
			if _, exists := result[k]; !exists {
				result[k] = v
				r.cache.Store(k, v)
			}
		}
	}

	return result, nil
}
