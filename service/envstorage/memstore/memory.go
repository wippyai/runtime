package memstore

import (
	"context"
	"sync"

	"go.uber.org/zap"
)

// MemoryStorage implements envstorage.Storage interface using in-memory map
type MemoryStorage struct {
	values map[string]string
	mutex  sync.RWMutex
	log    *zap.Logger
}

// NewMemoryStorage creates a new memory-based storage
func NewMemoryStorage(log *zap.Logger) *MemoryStorage {
	return &MemoryStorage{
		values: make(map[string]string),
		log:    log.With(zap.String("component", "memstorage")),
	}
}

// Get retrieves a value from storage
func (s *MemoryStorage) Get(_ context.Context, key string) (string, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	value, exists := s.values[key]
	return value, exists
}

// Set stores a value in storage
func (s *MemoryStorage) Set(_ context.Context, key, value string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.values[key] = value
	return nil
}

// Delete removes a value from storage
func (s *MemoryStorage) Delete(_ context.Context, key string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.values, key)
	return nil
}
