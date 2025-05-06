package memenvstore

import (
	"context"
	"sync"

	"go.uber.org/zap"
)

// MemoryStorage implements envstorage.Storage interface using in-memory map
type MemoryStorage struct {
	values sync.Map
	log    *zap.Logger
}

// NewMemoryStorage creates a new memory-based storage
func NewMemoryStorage(defaultValues map[string]string, log *zap.Logger) *MemoryStorage {
	values := sync.Map{}
	for key, value := range defaultValues {
		values.Store(key, value)
	}

	return &MemoryStorage{
		values: values,
		log:    log.With(zap.String("component", "memstorage")),
	}
}

// Get retrieves a value from storage
func (s *MemoryStorage) Get(_ context.Context, key string) (string, bool) {
	value, exists := s.values.Load(key)
	if !exists {
		return "", false
	}
	return value.(string), true
}

// Set stores a value in storage
func (s *MemoryStorage) Set(_ context.Context, key, value string) error {
	s.values.Store(key, value)
	return nil
}

// Delete removes a value from storage
func (s *MemoryStorage) Delete(_ context.Context, key string) error {
	s.values.Delete(key)
	return nil
}
