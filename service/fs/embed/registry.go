package embed

import (
	"io/fs"
	"sync"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/pack"
	systemfs "github.com/wippyai/runtime/system/fs"
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
		return systemfs.NewEmptyPackPathError()
	}
	if reader == nil {
		return systemfs.NewNilReaderError()
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

	return nil, systemfs.NewFilesystemNotFoundWithCauseError(id.String(), fs.ErrNotExist)
}

// Close implements embedapi.Registry.Close.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.readers = make(map[string]*pack.Reader)
	return nil
}
