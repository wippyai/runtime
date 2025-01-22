package manager

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"go.uber.org/zap"
)

// Libraries handles Lua library operations
type Libraries struct {
	log       *zap.Logger
	dtt       payload.Transcoder
	libraries map[registry.ID]*api.LibraryConfig
}

// NewLibraries creates a new library manager instance
func NewLibraries(dtt payload.Transcoder, logger *zap.Logger) *Libraries {
	return &Libraries{
		log:       logger,
		dtt:       dtt,
		libraries: make(map[registry.ID]*api.LibraryConfig),
	}
}

// Add adds a new library
func (m *Libraries) Add(ctx context.Context, entry registry.Entry) error {
	cfg := new(api.LibraryConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := m.libraries[entry.ID]; exists {
		return fmt.Errorf("library %s already exists", entry.ID)
	}

	m.libraries[entry.ID] = cfg
	m.log.Info("added library", zap.String("id", string(entry.ID)))

	return nil
}

// Update updates an existing library
func (m *Libraries) Update(ctx context.Context, entry registry.Entry) error {
	cfg := new(api.LibraryConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := m.libraries[entry.ID]; !exists {
		return fmt.Errorf("library %s not found", entry.ID)
	}

	m.libraries[entry.ID] = cfg
	m.log.Info("updated library", zap.String("id", string(entry.ID)))

	return nil
}

// Delete removes a library
func (m *Libraries) Delete(ctx context.Context, entry registry.Entry) error {
	if _, exists := m.libraries[entry.ID]; !exists {
		return fmt.Errorf("library %s not found", entry.ID)
	}

	delete(m.libraries, entry.ID)
	m.log.Info("deleted library", zap.String("id", string(entry.ID)))

	return nil
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

func (m *Libraries) unmarshalAndValidate(data payload.Payload, cfg interface{}) error {
	if err := m.dtt.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if validator, ok := cfg.(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
		}
	}

	return nil
}
