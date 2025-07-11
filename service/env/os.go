package env

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.uber.org/zap"
)

var ErrStorageReadOnly = errors.New("storage is read-only")

type OSStorage struct {
	log *zap.Logger
}

func NewOSStorage(log *zap.Logger) *OSStorage {
	return &OSStorage{
		log: log.With(zap.String("component", "osstorage")),
	}
}

func (s *OSStorage) Get(_ context.Context, key string) (string, error) {
	if val := os.Getenv(key); val != "" {
		return val, nil
	}
	return "", os.ErrNotExist
}

func (s *OSStorage) Set(_ context.Context, key, value string) error {
	return ErrStorageReadOnly
}

func (s *OSStorage) Delete(_ context.Context, key string) error {
	return ErrStorageReadOnly
}

func (s *OSStorage) List(_ context.Context) (map[string]string, error) {
	result := make(map[string]string)
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result, nil
}

func (s *OSStorage) Start(_ context.Context) (<-chan any, error) {
	statusCh := make(chan any, 1)
	statusCh <- supervisor.Running
	return statusCh, nil
}

func (s *OSStorage) Stop(_ context.Context) error {
	return nil
}

func (s *OSStorage) Acquire(_ context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	if mode != resource.ModeNormal {
		return nil, resource.ErrResourceLocked
	}

	return &osResource{
		storage: s,
		id:      id,
		closed:  false,
		mu:      sync.Mutex{},
	}, nil
}

type osResource struct {
	storage *OSStorage
	id      registry.ID
	closed  bool
	mu      sync.Mutex
}

func (r *osResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, resource.ErrResourceClosed
	}

	return r.storage, nil
}

func (r *osResource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}

	r.closed = true
}
