package embed

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"sync"

	"github.com/wippyai/runtime/api/registry"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	systemfs "github.com/wippyai/runtime/system/fs"
	"github.com/wippyai/wapp"
)

// Registry implements embedapi.Registry by storing Readers.
type Registry struct {
	readers map[string]*wapp.Reader
	files   []*os.File
	mu      sync.RWMutex
}

// NewRegistry creates a new embed registry.
func NewRegistry() *Registry {
	return &Registry{
		readers: make(map[string]*wapp.Reader),
	}
}

// Register adds a pack reader to the registry.
// The pack path is used as a key for later lookup.
// If a file handle is provided, it will be closed when the registry is closed.
func (r *Registry) Register(packPath string, reader *wapp.Reader, file *os.File) error {
	if packPath == "" {
		return systemfs.NewEmptyPackPathError()
	}
	if reader == nil {
		return systemfs.NewNilReaderError()
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.readers[packPath] = reader
	if file != nil {
		r.files = append(r.files, file)
	}
	return nil
}

// GetFS implements embedapi.Registry.GetFS.
// It searches all registered pack readers for a resource with the given ID.
func (r *Registry) GetFS(id registry.ID) (fs.ReadDirFS, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	wappID := wapp.NewID(id.NS, id.Name)

	// Search all pack readers for the resource
	for _, reader := range r.readers {
		fsys, err := reader.GetFS(wappID)
		if err == nil {
			return fsys, nil
		}
	}

	return nil, systemfs.NewFilesystemNotFoundWithCauseError(id.String(), fs.ErrNotExist)
}

// Close implements embedapi.Registry.Close.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error
	for _, f := range r.files {
		if err := f.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	r.files = nil
	r.readers = make(map[string]*wapp.Reader)

	if len(errs) > 0 {
		return fmt.Errorf("failed to close %d file(s)", len(errs))
	}
	return nil
}

// GetRegistryFromContext retrieves the concrete Registry from context.
// Returns nil if not found or if the registry is a different implementation.
func GetRegistryFromContext(ctx context.Context) *Registry {
	reg := embedapi.GetRegistry(ctx)
	if reg == nil {
		return nil
	}
	if r, ok := reg.(*Registry); ok {
		return r
	}
	return nil
}
