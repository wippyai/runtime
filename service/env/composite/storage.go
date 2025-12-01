package composite

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/env"
	"go.uber.org/zap"
)

type Storage struct {
	storages []env.Storage
	log      *zap.Logger
	cache    sync.Map
	mu       sync.RWMutex
}

func NewStorage(storages []env.Storage, log *zap.Logger) (*Storage, error) {
	if len(storages) == 0 {
		return nil, fmt.Errorf("at least one storage must be provided")
	}

	return &Storage{
		storages: storages,
		log:      log,
		cache:    sync.Map{},
	}, nil
}

func (r *Storage) Get(ctx context.Context, name string) (string, error) {
	if cachedValue, exists := r.cache.Load(name); exists {
		if value, ok := cachedValue.(string); ok {
			return value, nil
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var lastErr error
	for _, storage := range r.storages {
		value, err := storage.Get(ctx, name)

		if err == nil && value != "" {
			r.cache.Store(name, value)
			return value, nil
		}
		if err != nil {
			lastErr = err
		}
	}

	return "", lastErr
}

func (r *Storage) Set(ctx context.Context, name, value string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	err := r.storages[0].Set(ctx, name, value)
	if err == nil {
		r.cache.Store(name, value)
	}

	return err
}

func (r *Storage) Delete(ctx context.Context, name string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	err := r.storages[0].Delete(ctx, name)
	if err == nil {
		r.cache.Delete(name)
	}

	return err
}

func (r *Storage) List(ctx context.Context) (map[string]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]string)

	for i, storage := range r.storages {
		storageVars, err := storage.List(ctx)
		if err != nil {
			r.log.Warn("failed to list variables from storage", zap.Int("storage_index", i), zap.Error(err))
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
