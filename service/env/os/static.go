package os

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/env"
)

// StaticStorage provides a read-only environment storage backed by a predefined map.
// Unlike Storage which reads from actual OS environment variables, StaticStorage
// uses a fixed set of key-value pairs provided during initialization.
// This is useful for testing or sandboxed environments.
type StaticStorage struct {
	data map[string]string
	mu   sync.RWMutex
}

// Verify StaticStorage implements env.Storage
var _ env.Storage = (*StaticStorage)(nil)

// NewStaticStorage creates a new static storage with the given data.
// The data is copied to prevent external modifications.
func NewStaticStorage(data map[string]string) *StaticStorage {
	if data == nil {
		data = make(map[string]string)
	}

	dataCopy := make(map[string]string, len(data))
	for k, v := range data {
		dataCopy[k] = v
	}

	return &StaticStorage{
		data: dataCopy,
	}
}

// Get retrieves a value by key.
func (s *StaticStorage) Get(_ context.Context, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if val, ok := s.data[key]; ok {
		return val, nil
	}
	return "", env.ErrVariableNotFound
}

// Set is not supported for static storage.
func (s *StaticStorage) Set(_ context.Context, _, _ string) error {
	return env.ErrStorageReadOnly
}

// Delete is not supported for static storage.
func (s *StaticStorage) Delete(_ context.Context, _ string) error {
	return env.ErrStorageReadOnly
}

// List returns all key-value pairs.
func (s *StaticStorage) List(_ context.Context) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]string, len(s.data))
	for k, v := range s.data {
		result[k] = v
	}
	return result, nil
}
