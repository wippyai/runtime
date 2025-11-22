package env

import (
	"context"
	"os"
	"sync"

	"go.uber.org/zap"
)

type MemoryStorage struct {
	values sync.Map
	log    *zap.Logger
}

func NewMemoryStorage(defaultValues map[string]string, log *zap.Logger) *MemoryStorage {
	storage := &MemoryStorage{
		values: sync.Map{},
		log:    log,
	}

	for key, value := range defaultValues {
		storage.values.Store(key, value)
	}

	return storage
}

func (s *MemoryStorage) Get(_ context.Context, key string) (string, error) {
	value, exists := s.values.Load(key)
	if !exists {
		return "", os.ErrNotExist
	}

	strValue, ok := value.(string)
	if !ok {
		return "", os.ErrNotExist
	}

	return strValue, nil
}

func (s *MemoryStorage) Set(_ context.Context, key, value string) error {
	s.values.Store(key, value)
	return nil
}

func (s *MemoryStorage) Delete(_ context.Context, key string) error {
	s.values.Delete(key)
	return nil
}

func (s *MemoryStorage) List(_ context.Context) (map[string]string, error) {
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
