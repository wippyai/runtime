package filestore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"
)

// FileStorage implements envstorage.Storage interface using a JSON file
type FileStorage struct {
	path   string
	values map[string]string
	mutex  sync.RWMutex
	log    *zap.Logger
}

// NewFileStorage creates a new file-based storage
func NewFileStorage(path string, log *zap.Logger) *FileStorage {
	return &FileStorage{
		path:   path,
		values: make(map[string]string),
		log:    log.With(zap.String("component", "filestorage"), zap.String("path", path)),
	}
}

// Get retrieves a value from storage
func (s *FileStorage) Get(_ context.Context, key string) (string, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	value, exists := s.values[key]
	return value, exists
}

// Set stores a value in storage
func (s *FileStorage) Set(_ context.Context, key, value string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.values[key] = value
	return s.save()
}

// Delete removes a value from storage
func (s *FileStorage) Delete(_ context.Context, key string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	delete(s.values, key)
	return s.save()
}

// load reads the storage file and loads values into memory
func (s *FileStorage) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet, that's ok
			return nil
		}
		return err
	}

	return json.Unmarshal(data, &s.values)
}

// save writes the current values to the storage file
func (s *FileStorage) save() error {
	// Ensure directory exists
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.Marshal(s.values)
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0644)
}
