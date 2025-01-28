package manager

import (
	"fmt"

	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"go.uber.org/zap"
)

// Libraries handles Lua library operations
type Libraries struct {
	log       *zap.Logger
	libraries map[registry.ID]*api.LibraryConfig
}

// NewLibraries creates a new library manager instance
func NewLibraries(logger *zap.Logger) *Libraries {
	return &Libraries{
		log:       logger,
		libraries: make(map[registry.ID]*api.LibraryConfig),
	}
}

// Add adds a new library
func (m *Libraries) Add(id registry.ID, config *api.LibraryConfig) error {
	if _, exists := m.libraries[id]; exists {
		return fmt.Errorf("library %s already exists", id)
	}

	m.libraries[id] = config
	m.log.Info("added library", zap.String("id", string(id)))
	return nil
}

// Update updates an existing library
func (m *Libraries) Update(id registry.ID, config *api.LibraryConfig) error {
	if _, exists := m.libraries[id]; !exists {
		return fmt.Errorf("library %s not found", id)
	}

	m.libraries[id] = config
	m.log.Info("updated library", zap.String("id", string(id)))
	return nil
}

// Delete removes a library
func (m *Libraries) Delete(id registry.ID) error {
	if _, exists := m.libraries[id]; !exists {
		return fmt.Errorf("library %s not found", id)
	}

	delete(m.libraries, id)
	m.log.Info("deleted library", zap.String("id", string(id)))
	return nil
}

// Clone creates a reusable copy of the library manager
func (m *Libraries) Clone() *Libraries {
	cloned := &Libraries{
		log:       m.log,
		libraries: make(map[registry.ID]*api.LibraryConfig, len(m.libraries)),
	}

	// Copy map entries (pointers remain the same)
	for id, config := range m.libraries {
		cloned.libraries[id] = config
	}

	return cloned
}

// Get retrieves a library by ID
func (m *Libraries) Get(id registry.ID) (*api.LibraryConfig, error) {
	lib, exists := m.libraries[id]
	if !exists {
		return nil, fmt.Errorf("library %s not found", id)
	}

	return lib, nil
}

// Has checks if a library exists
func (m *Libraries) Has(id registry.ID) bool {
	_, exists := m.libraries[id]
	return exists
}
