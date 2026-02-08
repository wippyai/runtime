package memory

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/env"
)

// Storage is an in-memory implementation of env.Storage.
// It is safe for concurrent use.
type Storage struct {
	values sync.Map
}

// Verify Storage implements env.Storage
var _ env.Storage = (*Storage)(nil)

// NewStorage creates a new in-memory storage with optional default values.
func NewStorage(defaultValues map[string]string) *Storage {
	storage := &Storage{
		values: sync.Map{},
	}

	for key, value := range defaultValues {
		storage.values.Store(key, value)
	}

	return storage
}

// Get retrieves a value by key. Returns ErrVariableNotFound if not found.
func (s *Storage) Get(_ context.Context, key string) (string, error) {
	value, exists := s.values.Load(key)
	if !exists {
		return "", env.ErrVariableNotFound
	}

	strValue, ok := value.(string)
	if !ok {
		return "", env.ErrVariableNotFound
	}

	return strValue, nil
}

// Set stores a key-value pair.
func (s *Storage) Set(_ context.Context, key, value string) error {
	s.values.Store(key, value)
	return nil
}

// Delete removes a key from storage.
func (s *Storage) Delete(_ context.Context, key string) error {
	s.values.Delete(key)
	return nil
}

// List returns all key-value pairs.
func (s *Storage) List(_ context.Context) (map[string]string, error) {
	result := make(map[string]string)
	s.values.Range(func(key, value any) bool {
		if strKey, ok := key.(string); ok {
			if strValue, ok := value.(string); ok {
				result[strKey] = strValue
			}
		}
		return true
	})
	return result, nil
}
