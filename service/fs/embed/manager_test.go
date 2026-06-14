// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func TestManager_Add(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := &mockEmbedRegistry{
		filesystems: map[string]fs.ReadDirFS{
			"test:fs": &mockReadDirFS{},
		},
	}
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	config := &embedapi.Config{}
	entry := registry.Entry{
		ID:   registry.NewID("test", "fs"),
		Kind: embedapi.Kind,
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	// Verify filesystem was stored
	manager.mu.RLock()
	val, ok := manager.filesystems[entry.ID]
	manager.mu.RUnlock()
	assert.True(t, ok)
	assert.NotNil(t, val)
}

func TestManager_Add_DuplicateID(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := &mockEmbedRegistry{
		filesystems: map[string]fs.ReadDirFS{
			"test:fs": &mockReadDirFS{},
		},
	}
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	config := &embedapi.Config{}
	entry := registry.Entry{
		ID:   registry.NewID("test", "fs"),
		Kind: embedapi.Kind,
		Data: payload.New(config),
	}

	// Add first time
	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	// Try to add again
	err = manager.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestManager_Add_InvalidKind(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := NewRegistry()
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("test", "embed"),
		Kind: "invalid.kind",
		Data: payload.New(&embedapi.Config{}),
	}

	err := manager.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported entry kind")
}

func TestManager_Add_DecodeFailure(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := NewRegistry()
	dtt := &mockDTT{unmarshalErr: assert.AnError}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("test", "embed"),
		Kind: embedapi.Kind,
		Data: payload.New(&embedapi.Config{}),
	}

	err := manager.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode config")
}

func TestManager_Add_FSNotFound(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := NewRegistry()
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	config := &embedapi.Config{}
	entry := registry.Entry{
		ID:   registry.NewID("test", "notfound"),
		Kind: embedapi.Kind,
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get embedded filesystem")
}

func TestManager_Update(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := &mockEmbedRegistry{
		filesystems: map[string]fs.ReadDirFS{
			"test:fs": &mockReadDirFS{},
		},
	}
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	config := &embedapi.Config{}
	entry := registry.Entry{
		ID:   registry.NewID("test", "fs"),
		Kind: embedapi.Kind,
		Data: payload.New(config),
	}

	// Add first
	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	// Update
	err = manager.Update(ctx, entry)
	assert.NoError(t, err)

	// Verify still exists
	manager.mu.RLock()
	val, ok := manager.filesystems[entry.ID]
	manager.mu.RUnlock()
	assert.True(t, ok)
	assert.NotNil(t, val)
}

func TestManager_Update_NotFound(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := NewRegistry()
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("test", "notfound"),
		Kind: embedapi.Kind,
		Data: payload.New(&embedapi.Config{}),
	}

	err := manager.Update(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_Delete(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := &mockEmbedRegistry{
		filesystems: map[string]fs.ReadDirFS{
			"test:fs": &mockReadDirFS{},
		},
	}
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	config := &embedapi.Config{}
	entry := registry.Entry{
		ID:   registry.NewID("test", "fs"),
		Kind: embedapi.Kind,
		Data: payload.New(config),
	}

	// Add first
	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	// Delete
	err = manager.Delete(ctx, entry)
	assert.NoError(t, err)

	// Verify removed
	manager.mu.RLock()
	_, ok := manager.filesystems[entry.ID]
	manager.mu.RUnlock()
	assert.False(t, ok)
}

func TestManager_Delete_NotFound(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := NewRegistry()
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("test", "notfound"),
		Kind: embedapi.Kind,
		Data: payload.New(&embedapi.Config{}),
	}

	err := manager.Delete(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_Add_UsesEntryResolver(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := &mockEntryResolverRegistry{
		mockEmbedRegistry: mockEmbedRegistry{
			filesystems: map[string]fs.ReadDirFS{
				"test:fs": &mockReadDirFS{},
			},
		},
		byEntry: map[string]fs.ReadDirFS{
			"test:fs|org/mod|2.0.0": &mockReadDirFS{},
		},
	}
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("test", "fs"),
		Kind: embedapi.Kind,
		Meta: map[string]any{"module": "org/mod", "module_version": "2.0.0"},
		Data: payload.New(&embedapi.Config{}),
	}

	require.NoError(t, manager.Add(ctx, entry))
	assert.Equal(t, 1, embedReg.entryCalls)
	assert.Equal(t, 0, embedReg.idCalls)
}

func TestManager_Update_SelectsNewVersionViaResolver(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	oldFS := &mockReadDirFS{}
	newFS := &mockReadDirFS{}
	embedReg := &mockEntryResolverRegistry{
		mockEmbedRegistry: mockEmbedRegistry{
			filesystems: map[string]fs.ReadDirFS{"test:fs": oldFS},
		},
		byEntry: map[string]fs.ReadDirFS{
			"test:fs|org/mod|1.0.0": oldFS,
			"test:fs|org/mod|2.0.0": newFS,
		},
	}
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	addEntry := registry.Entry{
		ID:   registry.NewID("test", "fs"),
		Kind: embedapi.Kind,
		Meta: map[string]any{"module": "org/mod", "module_version": "1.0.0"},
		Data: payload.New(&embedapi.Config{}),
	}
	require.NoError(t, manager.Add(ctx, addEntry))

	updateEntry := addEntry
	updateEntry.Meta = map[string]any{"module": "org/mod", "module_version": "2.0.0"}
	require.NoError(t, manager.Update(ctx, updateEntry))

	manager.mu.RLock()
	stored := manager.filesystems[updateEntry.ID]
	manager.mu.RUnlock()
	// The stored FS is wrapped; assert resolver was asked for the new version.
	require.NotNil(t, stored)
	assert.Equal(t, "test:fs|org/mod|2.0.0", embedReg.lastEntryKey)
}

// Mock implementations

type mockEmbedRegistry struct {
	filesystems map[string]fs.ReadDirFS
}

func (r *mockEmbedRegistry) GetFS(id registry.ID) (fs.ReadDirFS, error) {
	if fsys, ok := r.filesystems[id.String()]; ok {
		return fsys, nil
	}
	return nil, errors.New("filesystem not found")
}

func (r *mockEmbedRegistry) Close() error {
	return nil
}

func (r *mockEmbedRegistry) Register(_ string, _ any) error {
	return nil
}

// mockEntryResolverRegistry implements embedapi.EntryResolver to verify the
// manager prefers entry-aware resolution.
type mockEntryResolverRegistry struct {
	mockEmbedRegistry
	byEntry      map[string]fs.ReadDirFS
	entryCalls   int
	idCalls      int
	lastEntryKey string
}

func entryResolverKey(entry registry.Entry) string {
	module, _ := entry.Meta["module"].(string)
	version, _ := entry.Meta["module_version"].(string)
	return entry.ID.String() + "|" + module + "|" + version
}

func (r *mockEntryResolverRegistry) GetFSForEntry(entry registry.Entry) (fs.ReadDirFS, error) {
	r.entryCalls++
	key := entryResolverKey(entry)
	r.lastEntryKey = key
	if fsys, ok := r.byEntry[key]; ok {
		return fsys, nil
	}
	return r.mockEmbedRegistry.GetFS(entry.ID)
}

func (r *mockEntryResolverRegistry) GetFS(id registry.ID) (fs.ReadDirFS, error) {
	r.idCalls++
	return r.mockEmbedRegistry.GetFS(id)
}

type mockDTT struct {
	unmarshalErr error
}

func (m *mockDTT) Unmarshal(p payload.Payload, v any) error {
	if m.unmarshalErr != nil {
		return m.unmarshalErr
	}
	if config, ok := v.(*embedapi.Config); ok {
		if src, ok := p.Data().(*embedapi.Config); ok {
			*config = *src
			return nil
		}
	}
	return nil
}

func (m *mockDTT) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

type mockReadDirFS struct{}

func (m *mockReadDirFS) Open(_ string) (fs.File, error) {
	return nil, fs.ErrNotExist
}

func (m *mockReadDirFS) ReadDir(_ string) ([]fs.DirEntry, error) {
	return nil, nil
}
