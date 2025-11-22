package env

import (
	"context"
	"errors"
	"os"
	"strings"

	"go.uber.org/zap"
)

var ErrStorageReadOnly = errors.New("storage is read-only")

type OSStorage struct {
	log *zap.Logger
}

func NewOSStorage(log *zap.Logger) *OSStorage {
	return &OSStorage{log: log}
}

func (s *OSStorage) Get(_ context.Context, key string) (string, error) {
	if val := os.Getenv(key); val != "" {
		return val, nil
	}
	return "", os.ErrNotExist
}

func (s *OSStorage) Set(_ context.Context, _, _ string) error {
	return ErrStorageReadOnly
}

func (s *OSStorage) Delete(_ context.Context, _ string) error {
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
