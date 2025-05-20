package env

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.uber.org/zap"
)

// FileStorage implements env.Storage interface using a JSON file
type FileStorage struct {
	filepath string
	mutex    sync.RWMutex
	log      *zap.Logger
}

// NewFileStorage creates a new file-based storage
func NewFileStorage(filepath string, log *zap.Logger) *FileStorage {
	return &FileStorage{
		filepath: filepath,
		log:      log.With(zap.String("component", "filestorage"), zap.String("filepath", filepath)),
	}
}

// Get retrieves a value from storage
func (s *FileStorage) Get(_ context.Context, key string) (string, error) {
	s.mutex.RLock()
	filepath := s.filepath
	s.mutex.RUnlock()

	// Open file for reading
	file, err := os.Open(filepath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// Read file line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		if commentIndex := strings.Index(line, "#"); commentIndex != -1 {
			line = line[:commentIndex]
		}
		line = strings.TrimSpace(line)

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		keyParsed := strings.TrimSpace(parts[0])
		valueParsed := strings.TrimSpace(parts[1])
		if keyParsed == key {
			return valueParsed, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", os.ErrNotExist
}

// Set stores a value in storage
func (s *FileStorage) Set(_ context.Context, key, value string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Read the file line by line and update the matching key
	tempPath := s.filepath + ".tmp"

	// Open source file for reading
	inFile, err := os.Open(s.filepath)
	if err != nil {
		return err
	}
	defer inFile.Close()

	// Create a temporary file for writing
	outFile, err := os.Create(tempPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	scanner := bufio.NewScanner(inFile)
	writer := bufio.NewWriter(outFile)
	updated := false

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)

		if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			// Found the key, update the value while preserving any comment
			commentIndex := strings.Index(line, "#")
			if commentIndex != -1 {
				fmt.Fprintf(writer, "%s=%s %s\n", key, value, line[commentIndex:])
			} else {
				fmt.Fprintf(writer, "%s=%s\n", key, value)
			}
			updated = true
		} else {
			// Keep the original line
			fmt.Fprintln(writer, line)
		}
	}

	if err = scanner.Err(); err != nil {
		return err
	}

	// If key wasn't found, append it
	if !updated {
		fmt.Fprintf(writer, "%s=%s\n", key, value)
	}

	// Flush writer before closing files
	if err := writer.Flush(); err != nil {
		return err
	}

	// Close files before rename
	inFile.Close()
	outFile.Close()

	// Replace the original file with a temporary file
	if err = os.Rename(tempPath, s.filepath); err != nil {
		return err
	}

	return nil
}

// Delete removes a value from storage
func (s *FileStorage) Delete(_ context.Context, key string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Read the file line by line and remove the matching key
	tempPath := s.filepath + ".tmp"

	// Open source file for reading
	inFile, err := os.Open(s.filepath)
	if err != nil {
		return err
	}
	defer inFile.Close()

	// Create a temporary file for writing
	outFile, err := os.Create(tempPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	scanner := bufio.NewScanner(inFile)
	writer := bufio.NewWriter(outFile)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)

		if len(parts) != 2 || strings.TrimSpace(parts[0]) != key {
			// Keep lines that don't match the key
			fmt.Fprintln(writer, line)
		}
	}

	if err = scanner.Err(); err != nil {
		return err
	}

	// Flush writer before closing files
	if err := writer.Flush(); err != nil {
		return err
	}

	// Close files before rename
	inFile.Close()
	outFile.Close()

	// Replace the original file with a temporary file
	if err = os.Rename(tempPath, s.filepath); err != nil {
		return err
	}

	return nil
}

// List returns all variable names and values in this storage
func (s *FileStorage) List(_ context.Context) (map[string]string, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	result := make(map[string]string)

	// Open file for reading
	file, err := os.Open(s.filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, err
	}
	defer file.Close()

	// Read file line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		if commentIndex := strings.Index(line, "#"); commentIndex != -1 {
			line = line[:commentIndex]
		}
		line = strings.TrimSpace(line)

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		keyParsed := strings.TrimSpace(parts[0])
		valueParsed := strings.TrimSpace(parts[1])

		result[keyParsed] = valueParsed
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// Start implements supervisor.Service interface
func (s *FileStorage) Start(_ context.Context) (<-chan any, error) {
	statusCh := make(chan any, 1)

	// Ensure directory exists
	dir := filepath.Dir(s.filepath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	statusCh <- supervisor.Running
	return statusCh, nil
}

// Stop implements supervisor.Service interface
func (s *FileStorage) Stop(_ context.Context) error {
	return nil
}

// Acquire implements resource.Provider interface
func (s *FileStorage) Acquire(_ context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, resource.ErrResourceLocked
	}

	return &fileResource{
		storage: s,
		id:      id,
		closed:  false,
		mu:      sync.Mutex{},
	}, nil
}

// fileResource represents an acquired file storage resource
type fileResource struct {
	storage *FileStorage
	id      registry.ID
	closed  bool
	mu      sync.Mutex
}

// Get implements resource.Resource interface
func (r *fileResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, resource.ErrResourceClosed
	}

	return r.storage, nil
}

// Release implements resource.Resource interface
func (r *fileResource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}

	r.closed = true
}
