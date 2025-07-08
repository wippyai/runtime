package registry

import (
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Define error constants that match the registry API
// var (
//	ErrEntryNotFound   = errors.New("entry not found")
//	ErrVersionNotFound = errors.New("version not found")
//)
//
//// mockRegistry implements regapi.Registry for testing
// type mockRegistry struct {
//	entries map[regapi.ID]regapi.Entry
//	version uint
//	history []regapi.Version
//}
//
// func newMockRegistry() *mockRegistry {
//	return &mockRegistry{
//		entries: make(map[regapi.ID]regapi.Entry),
//		version: 1,
//		history: []regapi.Version{
//			&mockVersion{id: 1},
//		},
//	}
//}
//
// func (m *mockRegistry) GetEntry(id regapi.ID) (regapi.Entry, error) {
//	entry, exists := m.entries[id]
//	if !exists {
//		return regapi.Entry{}, ErrEntryNotFound
//	}
//	return entry, nil
//}
//
// func (m *mockRegistry) GetAllEntries() ([]regapi.Entry, error) {
//	entries := make([]regapi.Entry, 0, len(m.entries))
//	for _, entry := range m.entries {
//		entries = append(entries, entry)
//	}
//	return entries, nil
//}
//
// func (m *mockRegistry) Current() (regapi.Version, error) {
//	return &mockVersion{id: m.version}, nil
//}
//
// func (m *mockRegistry) History() regapi.History {
//	return &mockHistory{versions: m.history}
//}
//
// func (m *mockRegistry) Apply(ctx context.Context, changes regapi.ChangeSet) (regapi.Version, error) {
//	// Simple implementation for testing
//	m.version++
//	return &mockVersion{id: m.version}, nil
//}
//
// func (m *mockRegistry) ApplyVersion(ctx context.Context, version regapi.Version) error {
//	// Simple implementation for testing
//	return nil
//}
//
// mockVersion implements regapi.Version for testing
// type mockVersion struct {
//	id uint
//}
//
// func (m *mockVersion) ID() uint {
//	return m.id
//}
//
// func (m *mockVersion) Previous() regapi.Version {
//	if m.id <= 1 {
//		return nil
//	}
//	return &mockVersion{id: m.id - 1}
//}
//
// func (m *mockVersion) String() string {
//	return "v1"
//}
//
//// mockHistory implements regapi.History for testing
// type mockHistory struct {
//	versions []regapi.Version
//}
//
// func (m *mockHistory) Versions() ([]regapi.Version, error) {
//	return m.versions, nil
//}
//
// func (m *mockHistory) Get(version regapi.Version) (regapi.ChangeSet, error) {
//	for _, v := range m.versions {
//		if v.ID() == version.ID() {
//			return regapi.ChangeSet{}, nil
//		}
//	}
//	return nil, ErrVersionNotFound
//}
//
// func (m *mockHistory) Save(v regapi.Version, cs regapi.ChangeSet, head bool) error {
//	return nil
//}
//
// func (m *mockHistory) Head() (regapi.Version, error) {
//	if len(m.versions) == 0 {
//		return nil, errors.New("no head version")
//	}
//	return m.versions[len(m.versions)-1], nil
//}

func TestRegistryModule(t *testing.T) {
	t.Run("module loader registers functions", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewRegistryModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		// Register the Registry module
		vm.State().PreloadModule(module.Name(), module.Loader)

		// Check that the module name is correct
		assert.Equal(t, "registry", module.Name())

		// Load the module and check that functions are registered
		err = vm.State().DoString(`
			local registry = require("registry")
			
			-- Check that core functions exist
			assert(type(registry.snapshot) == "function", "registry.snapshot should be a function")
			assert(type(registry.snapshot_at) == "function", "registry.snapshot_at should be a function")
			assert(type(registry.current_version) == "function", "registry.current_version should be a function")
			assert(type(registry.versions) == "function", "registry.versions should be a function")
			assert(type(registry.apply_version) == "function", "registry.apply_version should be a function")
			assert(type(registry.parse_id) == "function", "registry.parse_id should be a function")
			assert(type(registry.history) == "function", "registry.history should be a function")
			assert(type(registry.find) == "function", "registry.find should be a function")
			assert(type(registry.get) == "function", "registry.get should be a function")
			assert(type(registry.build_delta) == "function", "registry.build_delta should be a function")
		`)
		require.NoError(t, err)
	})

	t.Run("parse_id creates ID from string", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewRegistryModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		// Register the Registry module
		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test parse_id function
		err = vm.State().DoString(`
			local registry = require("registry")
			local id = registry.parse_id("test:example")
			
			-- Check that we got a table
			assert(type(id) == "table", "id should be a table")
			assert(id.ns == "test", "ns should be 'test'")
			assert(id.name == "example", "name should be 'example'")
		`)
		require.NoError(t, err)
	})

	t.Run("parse_id handles invalid format", func(t *testing.T) {
		logger := zap.NewNop()
		module := NewRegistryModule(logger)

		vm, err := engine.NewCVM(logger)
		require.NoError(t, err)
		defer vm.Close()

		// Register the Registry module
		vm.State().PreloadModule(module.Name(), module.Loader)

		// Test parse_id function with invalid format
		err = vm.State().DoString(`
			local registry = require("registry")
			local id = registry.parse_id("invalid_format")
			
			-- Should still return a table with empty values
			assert(type(id) == "table", "id should be a table")
			assert(id.ns == "", "ns should be empty for invalid format")
			assert(id.name == "invalid_format", "name should be the full string for invalid format")
		`)
		require.NoError(t, err)
	})
}
