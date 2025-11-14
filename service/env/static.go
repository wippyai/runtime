package env

import (
	"context"
	"os"
	"sync"

	"github.com/wippyai/runtime/api/supervisor"
	"go.uber.org/zap"
)

// StaticStorage provides a read-only environment storage backed by a predefined map.
// Unlike OSStorage which reads from actual OS environment variables, StaticStorage
// uses a fixed set of key-value pairs provided during initialization.
type StaticStorage struct {
	log  *zap.Logger
	mu   sync.RWMutex
	data map[string]string
}

func NewStaticStorage(data map[string]string, log *zap.Logger) *StaticStorage {
	if data == nil {
		data = make(map[string]string)
	}

	// Create a copy to prevent external modifications
	dataCopy := make(map[string]string, len(data))
	for k, v := range data {
		dataCopy[k] = v
	}

	return &StaticStorage{
		log:  log,
		data: dataCopy,
	}
}

func (s *StaticStorage) Get(_ context.Context, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if val, ok := s.data[key]; ok {
		return val, nil
	}
	return "", os.ErrNotExist
}

func (s *StaticStorage) Set(_ context.Context, _, _ string) error {
	return ErrStorageReadOnly
}

func (s *StaticStorage) Delete(_ context.Context, _ string) error {
	return ErrStorageReadOnly
}

func (s *StaticStorage) List(_ context.Context) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]string, len(s.data))
	for k, v := range s.data {
		result[k] = v
	}
	return result, nil
}

func (s *StaticStorage) Start(_ context.Context) (<-chan any, error) {
	statusCh := make(chan any, 1)
	statusCh <- supervisor.Running
	return statusCh, nil
}

func (s *StaticStorage) Stop(_ context.Context) error {
	return nil
}
