package embed

import (
	"fmt"
	"io/fs"
	"sync"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/pack"
)

// Registry implements embedapi.Registry by storing Readers.
type Registry struct {
	mu      sync.RWMutex
	readers map[string]*pack.Reader // packPath -> Reader
}

// NewRegistry creates a new embed registry.
func NewRegistry() *Registry {
	return &Registry{
		readers: make(map[string]*pack.Reader),
	}
}

// Register adds a pack reader to the registry.
// The pack path is used as a key for later lookup.
func (r *Registry) Register(packPath string, reader *pack.Reader) error {
	if packPath == "" {
		return fmt.Errorf("packPath cannot be empty")
	}
	if reader == nil {
		return fmt.Errorf("reader cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.readers[packPath] = reader
	return nil
}

// GetFS implements embedapi.Registry.GetFS.
// It searches all registered pack readers for a resource with the given ID.
func (r *Registry) GetFS(id registry.ID) (fs.ReadDirFS, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Search all pack readers for the resource
	for _, reader := range r.readers {
		fsys, err := reader.GetFS(id)
		if err == nil {
			return fsys, nil
		}
		// Continue searching if not found in this pack
	}

	return nil, fmt.Errorf("embedded filesystem not found: %s: %w", id, fs.ErrNotExist)
}

// Close implements embedapi.Registry.Close.
// It closes all pack readers.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Note: Reader doesn't currently have a Close method
	// If it's added in the future, we should close all readers here
	r.readers = make(map[string]*pack.Reader)
	return nil
}
