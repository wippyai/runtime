// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	eventapi "github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
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

func TestManager_Update_ResolutionFailureKeepsExistingFS(t *testing.T) {
	bus := &recordingBus{}
	embedReg := &mockEntryResolverRegistry{
		mockEmbedRegistry: mockEmbedRegistry{},
		byEntry: map[string]fs.ReadDirFS{
			"test:fs|org/mod|1.0.0": &mockReadDirFS{},
		},
	}
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())
	entry := registry.Entry{
		ID:   registry.NewID("test", "fs"),
		Kind: embedapi.Kind,
		Data: payload.New(&embedapi.Config{}),
	}
	require.NoError(t, manager.Add(opContext("org/mod", "1.0.0"), entry))
	bus.reset()

	err := manager.Update(opContext("org/mod", "2.0.0"), entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get embedded filesystem")

	manager.mu.RLock()
	_, ok := manager.filesystems[entry.ID]
	manager.mu.RUnlock()
	assert.True(t, ok, "failed update must keep the current live filesystem")
	assert.Equal(t, "test:fs|org/mod|2.0.0", embedReg.lastEntryKey)
	assert.Empty(t, bus.events, "failed update must not send fs delete/register events")
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
		Data: payload.New(&embedapi.Config{}),
	}

	require.NoError(t, manager.Add(opContext("org/mod", "2.0.0"), entry))
	assert.Equal(t, 1, embedReg.entryCalls)
	assert.Equal(t, 0, embedReg.idCalls)
	assert.Equal(t, "test:fs|org/mod|2.0.0", embedReg.lastEntryKey)
}

// A host entry arrives without provenance and resolves by ID, the same path
// a caller outside a transition takes.
func TestManager_Add_HostEntryResolvesWithoutProvenance(t *testing.T) {
	embedReg := &mockEntryResolverRegistry{
		mockEmbedRegistry: mockEmbedRegistry{
			filesystems: map[string]fs.ReadDirFS{"test:fs": &mockReadDirFS{}},
		},
	}

	manager := NewManager(eventbus.NewBus(), &mockDTT{}, embedReg, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("test", "fs"),
		Kind: embedapi.Kind,
		Data: payload.New(&embedapi.Config{}),
	}

	require.NoError(t, manager.Add(context.Background(), entry))
	assert.Equal(t, 1, embedReg.entryCalls)
	assert.Equal(t, "test:fs||", embedReg.lastEntryKey)
}

func TestManager_Update_SelectsNewVersionViaResolver(t *testing.T) {
	bus := &recordingBus{}
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
		Data: payload.New(&embedapi.Config{}),
	}
	require.NoError(t, manager.Add(opContext("org/mod", "1.0.0"), addEntry))
	bus.reset()

	updateEntry := addEntry
	require.NoError(t, manager.Update(opContext("org/mod", "2.0.0"), updateEntry))

	manager.mu.RLock()
	stored := manager.filesystems[updateEntry.ID]
	manager.mu.RUnlock()
	// The stored FS is wrapped; assert resolver was asked for the new version.
	require.NotNil(t, stored)
	assert.Equal(t, "test:fs|org/mod|2.0.0", embedReg.lastEntryKey)
	require.Len(t, bus.events, 2)
	assert.Equal(t, fsapi.FsDelete, bus.events[0].Kind)
	assert.Equal(t, updateEntry.ID.String(), bus.events[0].Path)
	assert.Equal(t, fsapi.FsRegister, bus.events[1].Kind)
	assert.Equal(t, updateEntry.ID.String(), bus.events[1].Path)
	assert.NotNil(t, bus.events[1].Data)
}

// Mock implementations

type recordingBus struct {
	events []eventapi.Event
}

func (b *recordingBus) Subscribe(context.Context, eventapi.System, chan<- eventapi.Event) (eventapi.SubscriberID, error) {
	return "", nil
}

func (b *recordingBus) SubscribeP(
	context.Context,
	eventapi.System,
	eventapi.Kind,
	chan<- eventapi.Event,
) (eventapi.SubscriberID, error) {
	return "", nil
}

func (b *recordingBus) Unsubscribe(context.Context, eventapi.SubscriberID) {}

func (b *recordingBus) Send(_ context.Context, evt eventapi.Event) {
	b.events = append(b.events, evt)
}

func (b *recordingBus) reset() {
	b.events = nil
}

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
	lastEntryKey string
	entryCalls   int
	idCalls      int
}

func entryResolverKey(entry registry.Entry, prov *registry.EntryProvenance) string {
	module := ""
	version := ""
	if prov != nil {
		module = prov.Module
		version = prov.Version
	}
	return entry.ID.String() + "|" + module + "|" + version
}

// opContext builds the context a transition hands a listener for an operation
// owned by a module.
func opContext(module, version string) context.Context {
	return registry.WithOpProvenance(context.Background(), registry.OpProvenance{
		Effective: &registry.EntryProvenance{Module: module, Version: version},
	})
}

func (r *mockEntryResolverRegistry) GetFSForEntry(
	entry registry.Entry,
	prov *registry.EntryProvenance,
) (fs.ReadDirFS, error) {
	r.entryCalls++
	key := entryResolverKey(entry, prov)
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
