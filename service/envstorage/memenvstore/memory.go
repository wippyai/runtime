package memenvstore

import (
	"context"
	"sync"

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
