// SPDX-License-Identifier: MPL-2.0

package composite

import (
	"context"
	"errors"
	"sync"

	"github.com/wippyai/runtime/api/env"
)

// Storage combines multiple storages with fallback and caching.
// Get operations search storages in order until a value is found.
// Set/Delete operations apply only to the first (primary) storage.
type Storage struct {
	cache    sync.Map
	storages []env.Storage
	mu       sync.RWMutex
}

// Verify Storage implements env.Storage
var _ env.Storage = (*Storage)(nil)

// NewStorage creates a composite storage from multiple underlying storages.
// At least one storage must be provided.
func NewStorage(storages []env.Storage) (*Storage, error) {
	if len(storages) == 0 {
		return nil, env.ErrNoStorages
	}

	copied := make([]env.Storage, len(storages))
	copy(copied, storages)

	return &Storage{
		storages: copied,
	}, nil
}

// Get retrieves a value by searching storages in order.
// The first storage that has the key (returns nil error) is used.
// Empty string is a valid value.
func (s *Storage) Get(ctx context.Context, name string) (string, error) {
	if cachedValue, exists := s.cache.Load(name); exists {
		if value, ok := cachedValue.(string); ok {
			return value, nil
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, storage := range s.storages {
		value, err := storage.Get(ctx, name)
		if err == nil {
			s.cache.Store(name, value)
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
func (s *Storage) Set(ctx context.Context, name, value string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	err := s.storages[0].Set(ctx, name, value)
	if err == nil {
		s.cache.Store(name, value)
	}

	return err
}

// Delete removes a value from the primary (first) storage.
func (s *Storage) Delete(ctx context.Context, name string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	err := s.storages[0].Delete(ctx, name)
	if err == nil {
		s.cache.Delete(name)
	}

	return err
}

// List returns all variables from all storages.
// Variables from earlier storages take precedence over later ones.
func (s *Storage) List(ctx context.Context) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]string)

	for _, storage := range s.storages {
		storageVars, err := storage.List(ctx)
		if err != nil {
			continue
		}

		for k, v := range storageVars {
			if _, exists := result[k]; !exists {
				result[k] = v
				s.cache.Store(k, v)
			}
		}
	}

	return result, nil
}
